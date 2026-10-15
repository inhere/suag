package chat

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/inhere/suga/internal/chat/lineedit"
	"github.com/inhere/suga/internal/personal/process"
	"golang.org/x/term"
)

type chatInputEvent struct {
	activity bool
	line     string
	err      error
	injected bool
	// key is what a steer read reports when a key, not a message, was pressed
	// during a turn (turnKey).
	key turnKey
	// settled (deliveries only) reports the final fate of a queued delivery: nil once it starts
	// a turn, an error if it is discarded at exit. Buffered(1); nil when nobody waits.
	settled chan error
}

// settle reports the delivery's final fate to a waiting sender; never blocks.
func (e chatInputEvent) settle(err error) {
	if e.settled == nil {
		return
	}
	select {
	case e.settled <- err:
	default:
	}
}

// chatInputRequest asks the reader goroutine for one read. The mode of that read is
// the source's readMode (see request): one read at a time, and a read in flight can
// change mode (ask.go) without being restarted.
type chatInputRequest struct {
	prompt string
}

// chatInputSource reads exactly one prompt per request and signals its first byte before
// forwarding it to the line editor, so callers can interrupt a waiting Stop hook first.
type chatInputSource struct {
	ctx    context.Context
	cancel context.CancelFunc
	input  io.Reader
	output io.Writer
	// makeRaw switches the tty to raw mode and returns the restore func; injectable so tests
	// need no real tty. nil when input is not a terminal.
	makeRaw    func() (restore func() error, err error)
	restoreRaw func() error // non-nil only while the tty is raw (guarded by mu)
	// turnActive is true from SetBusy(true) until the next Request: while set the tty must stay
	// cooked so Ctrl-C raises SIGINT, even if a line read started earlier is still blocked.
	turnActive bool
	// screenWidth is the terminal's column count when it could be read (0: unknown).
	screenWidth int
	// inputFD is the terminal's file descriptor, set with screenWidth: the platform
	// helpers (flushInput, inputWait, restoreOutputProcessing) work on it.
	inputFD int
	// steering keeps the keyboard readable while a turn runs (chat.steering):
	// SetBusy(true) then keeps raw input and readTurnKeys reads the keys.
	steering bool
	closed   bool // set by DiscardQueued/Close: deliveries are refused afterwards (guarded by mu)
	terminal *lineedit.Terminal
	requests chan chatInputRequest
	// events carries the result of a prompt read (Next); answers the answer of a
	// question (Ask) and turnEvents what a steer read reports (S2b). Each mode has
	// its own channel, so an answer can never be taken as a prompt line or as an
	// inserted message (design N5). All buffered(1).
	events     chan chatInputEvent
	answers    chan chatInputEvent
	turnEvents chan chatInputEvent
	// readMode is the mode of the read the reader is serving (guarded by mu): it
	// decides the activity handshake, the lone-Esc decision and the channel a result
	// is delivered to. request sets it, Ask switches it to modeAsk.
	readMode inputMode
	// inputWait reports whether more input is ready within the timeout, without
	// reading it: the wait behind the lone-Esc and the pasted-newline decisions of
	// the byte pump. nil without a terminal.
	inputWait func(timeout time.Duration) bool
	// readErr is the input's end (io.EOF) once it has ended: every later request
	// answers it at once instead of blocking in a read.
	readErr error
	// lastInterrupt and interruptSeen time the Ctrl-C presses that confirm leaving
	// chat (exitConfirmWindow); the prompt read and the steer read share them (m7).
	lastInterrupt time.Time
	interruptSeen bool
	activity      chan struct{}
	deliveries    chan chatInputEvent
	// taskWake signals a background task end (tasks.wake_on_exit); nil when
	// off. taskNotice takes the pending notices as the text of an injected
	// turn ("" when the running turn already took them).
	taskWake   <-chan struct{}
	taskNotice func() string
	done       chan struct{}
	mu         sync.Mutex
	pending    bool
	busy       bool
	reader     *chatActivityReader

	// Approval questions (ask.go). flushInput drops terminal input that was
	// typed but not read yet; injectable, nil when input is not a terminal.
	flushInput func() error
	askMu      sync.Mutex // one question at a time
	// cond (on mu) signals handing changes.
	cond *sync.Cond
	// inputEpoch is bumped by Ask right after flushing: a byte read from the
	// terminal before that is dropped instead of reaching the line editor.
	inputEpoch uint64
	// handing is set while the reader has passed a byte to the line editor
	// that the editor has not processed yet.
	handing bool
	// savedLine is a half-typed line taken out of a pending read for a
	// question; it prefills the next prompt read.
	savedLine string
}

func newChatInputSource(parent context.Context, input io.Reader, output io.Writer) (*chatInputSource, error) {
	ctx, cancel := context.WithCancel(parent)
	source := &chatInputSource{
		ctx: ctx, cancel: cancel, input: input, output: &lockedWriter{w: output},
		requests: make(chan chatInputRequest), events: make(chan chatInputEvent, 1),
		answers: make(chan chatInputEvent, 1), turnEvents: make(chan chatInputEvent, 1),
		activity: make(chan struct{}), deliveries: make(chan chatInputEvent, 8), done: make(chan struct{}),
	}
	source.cond = sync.NewCond(&source.mu)
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fd := int(file.Fd())
		// Raw mode is entered per line in readLine, not here: during model/tool execution the
		// terminal must stay cooked so Ctrl-C raises SIGINT and stderr keeps CR translation.
		source.makeRaw = func() (func() error, error) {
			state, err := term.MakeRaw(fd)
			if err != nil {
				return nil, err
			}
			return func() error { return term.Restore(fd, state) }, nil
		}
		source.flushInput = func() error { return flushTerminalInput(fd) }
		// The wait behind the lone-Esc and pasted-newline decisions: "is a byte
		// ready?", asked without reading it (inputwait_*.go).
		source.inputWait = func(timeout time.Duration) bool { return waitTerminalInput(fd, timeout) }
		source.reader = &chatActivityReader{source: source, input: input}
		terminalIO := &chatTerminalIO{reader: source.reader, output: source.output}
		source.terminal = lineedit.NewTerminal(terminalIO, "")
		// A lone Esc during a turn is the Esc key (end this turn). The pump declares
		// it before handing the byte over; prompt and ask reads never declare one.
		source.terminal.EscCallback = source.escKeyPressed
		// The terminal's real size drives the line editor's layout: it wraps a line
		// at termWidth and the highlighted rows are filled to the same width. Without
		// this the editor keeps wrapping at lineedit.DefaultWidth (80) however wide
		// the window is, so a window wider than 80 columns wrapped a line early and
		// the fill width (screenWidth) disagreed with the layout.
		if width, height, err := term.GetSize(fd); err == nil && width > 0 {
			source.screenWidth = width
			// The editor is empty here, so SetSize only records the size (it repaints
			// nothing); a write failure is cosmetic and must not stop the chat.
			_ = source.terminal.SetSize(width, height)
		}
		source.inputFD = fd
		// A window resized while chat runs: follow it, so the editor keeps wrapping
		// at the current width (SIGWINCH on Unix, a poll where there is no signal).
		go source.watchScreenSize(fd)
	}
	go source.run()
	return source, nil
}

func (s *chatInputSource) Deliver(text, from string) (bool, error) {
	queued, _, err := s.DeliverWait(text, from)
	return queued, err
}

// DeliverWait enqueues a message for the chat loop. settled yields nil once the message starts
// a turn, or process.ErrDiscarded if the chat exits while it is still queued.
func (s *chatInputSource) DeliverWait(text, from string) (bool, <-chan error, error) {
	if strings.TrimSpace(text) == "" {
		return false, nil, fmt.Errorf("delivery text is empty")
	}
	settled := make(chan error, 1)
	event := chatInputEvent{line: deliveryLine(text, from), injected: true, settled: settled}
	// Hold mu across the closed check and the enqueue so DiscardQueued cannot miss a delivery.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, nil, process.ErrSessionClosed
	}
	queued := s.busy || len(s.deliveries) > 0
	select {
	case s.deliveries <- event:
		return queued, settled, nil
	default:
		return true, nil, process.ErrQueueFull
	}
}

// DiscardQueued refuses further deliveries and fails every message still queued (its sender,
// if waiting, learns it was not delivered). It returns how many were dropped.
func (s *chatInputSource) DiscardQueued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	n := 0
	for {
		select {
		case event := <-s.deliveries:
			event.settle(process.ErrDiscarded)
			n++
		default:
			return n
		}
	}
}

// deliveryLine labels a delivered message with its source: "[来自 <from>] text".
// Text that already starts with a "[...] " tag (the sender labeled it itself)
// is kept as is.
func deliveryLine(text, from string) string {
	if hasSourceTag(text) {
		return text
	}
	return "[来自 " + safeSender(from) + "] " + text
}

// hasSourceTag reports whether text starts with a non-empty "[label]" on its first line
// followed by a space.
func hasSourceTag(text string) bool {
	if !strings.HasPrefix(text, "[") {
		return false
	}
	firstLine, _, _ := strings.Cut(text, "\n")
	end := strings.IndexByte(firstLine, ']')
	return end > 1 && end+1 < len(firstLine) && firstLine[end+1] == ' '
}

// safeSender is the source label: the request's From (`suga send --source`),
// "unknown" when empty, with control characters dropped and at most 64 runes.
func safeSender(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return "unknown"
	}
	clean := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, from)
	runes := []rune(clean)
	if len(runes) > 64 {
		clean = string(runes[:64])
	}
	return clean
}

// SetBusy marks a turn start/end. Starting a turn without chat.steering forces the tty back to
// cooked mode even when a line read begun earlier is still blocked (a `suga send` delivery can
// start a turn while the prompt is waiting); otherwise Ctrl-C would arrive as byte 0x03 and never
// interrupt the turn. With chat.steering (on by default) the turn keeps raw input and reads the
// keys instead: Esc and Ctrl-C then end the turn as bytes, without depending on the console
// turning Ctrl-C into a CTRL_C_EVENT - the path that fails inside a ConPTY (suga-nrf2).
func (s *chatInputSource) SetBusy(busy bool) {
	s.mu.Lock()
	s.busy = busy
	switch {
	case busy:
		s.turnActive = true
		if s.steering {
			// Raw input clears the output flags chat's own output needs (the line
			// editor writes its own CRLF, tool results and notices do not), so put
			// them back right away.
			_ = s.enterRawLocked()
			_ = restoreOutputProcessing(s.inputFD)
		} else {
			_ = s.leaveRawLocked()
		}
	case s.steering:
		// Between turns: cooked again until the next prompt read asks for raw mode
		// (request), and the read a turn borrowed echoes again - request draws its
		// prompt. A read still blocked from the turn goes on, cooked, like any read
		// that a delivery started.
		_ = s.leaveRawLocked()
		if s.terminal != nil {
			s.terminal.SetEcho(true)
		}
	}
	s.mu.Unlock()
}

// SetInputHighlight paints the line the person submits at the prompt
// (chat.highlight_input): the style is written around a repaint of the line, its
// background reaching the end of every terminal row. A zero style turns it off;
// input that is not a terminal has no line to paint.
func (s *chatInputSource) SetInputHighlight(style lineedit.InputStyle) {
	if s.terminal != nil {
		s.terminal.SetInputStyle(style)
	}
}

// highlightWidth is the number of columns chat fills a highlighted line to: the
// terminal's width when it reports one, else the width the line editor lays
// lines out with. 0 when input is not a terminal (no line editing, no filling).
// It is re-read per call, so a resized window is taken into account.
func (s *chatInputSource) highlightWidth() int {
	if s.terminal == nil {
		return 0
	}
	if width := s.currentScreenWidth(); width > 0 {
		return width
	}
	return lineedit.DefaultWidth
}

// currentScreenWidth is the last terminal width chat read (0: unknown).
func (s *chatInputSource) currentScreenWidth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screenWidth
}

// watchScreenSize applies window size changes for as long as chat runs. The line
// editor wraps at the size it was last given, so a resized window has to reach it
// (SetSize also repaints the line being edited; a blocked read releases the
// editor's lock, so that normally applies at once).
func (s *chatInputSource) watchScreenSize(fd int) {
	events, stop := resizeNotify()
	defer stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-events:
			s.applyScreenSize(fd)
		}
	}
}

// applyScreenSize re-reads the terminal size and hands a changed one to the line
// editor. A window that did not change costs one ioctl and nothing else.
func (s *chatInputSource) applyScreenSize(fd int) {
	if s.terminal == nil {
		return
	}
	width, height, err := term.GetSize(fd)
	if err != nil || width <= 0 {
		return
	}
	s.mu.Lock()
	changed := width != s.screenWidth
	s.screenWidth = width
	s.mu.Unlock()
	if !changed {
		return
	}
	_ = s.terminal.SetSize(width, height)
}

// Request asks for one prompt line (between turns): the chat loop takes the result
// from Next.
func (s *chatInputSource) Request(prompt string) bool { return s.request(prompt, modePrompt) }

// RequestSteer asks for a read during a turn (S2b): the input stays available while
// the model works, Esc / Ctrl-C ask to end the turn and Enter submits a line (which
// runTurn keeps for the next prompt). Results arrive on TurnEvents; runTurn starts it
// at the beginning of a turn when chat.steering is on and reacts to what it reports.
// The read draws nothing (ReadLineNoEcho): a turn has no input line on the screen.
func (s *chatInputSource) RequestSteer(prompt string) bool {
	return s.request(prompt, modeSteer)
}

// request asks the reader goroutine for one read in mode. The mode is set here, under
// the same lock as pending: a read already waiting for input continues as this read
// (only its prompt and mode change), a finished one starts a new request — and between
// the two nothing reads, so input typed meanwhile is never taken early.
func (s *chatInputSource) request(prompt string, mode inputMode) bool {
	s.mu.Lock()
	s.readMode = mode
	// Back at a prompt: raw mode is allowed again. If a read from before the turn is still
	// blocked it continues as this prompt's read, so switch to raw for it right here.
	s.turnActive = false
	if s.pending {
		err := s.enterRawLocked()
		s.mu.Unlock()
		_ = err // best effort: the pending read still works in cooked mode
		// The read keeps its line; only the prompt changes (e.g. a Stop hook that
		// returned without waiting for an external reply).
		if s.terminal != nil {
			_ = s.terminal.UpdatePrompt(prompt)
		}
		return true
	}
	s.pending = true
	s.mu.Unlock()
	select {
	case s.requests <- chatInputRequest{prompt: prompt}:
		return true
	case <-s.ctx.Done():
		s.mu.Lock()
		s.pending = false
		s.mu.Unlock()
		return false
	}
}

func (s *chatInputSource) Next() <-chan chatInputEvent { return s.events }

// TurnEvents is where a steer read reports what the person asked for during a turn:
// a line for an inserted message, a key for abort/exit. The turn consumer reads it
// (S2b); while chat.steering is off nothing is written to it.
func (s *chatInputSource) TurnEvents() <-chan chatInputEvent { return s.turnEvents }

func (s *chatInputSource) AckActivity() {
	select {
	case s.activity <- struct{}{}:
	case <-s.ctx.Done():
	}
}

func (s *chatInputSource) CompleteLine() {
	s.mu.Lock()
	s.pending = false
	s.mu.Unlock()
}

func (s *chatInputSource) Write(p []byte) (int, error) {
	if s.terminal != nil {
		return s.terminal.Write(p)
	}
	return s.output.Write(p)
}

func (s *chatInputSource) Close() error {
	s.cancel()
	// The reader goroutine may still be blocked inside a raw-mode line edit; restore for it and
	// keep it cooked from now on.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnActive = true
	s.closed = true
	return s.leaveRawLocked()
}

// enterRaw switches the tty to raw mode for line editing (no-op if already raw, or while a turn
// is running, when the terminal must stay cooked).
func (s *chatInputSource) enterRaw() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enterRawLocked()
}

func (s *chatInputSource) enterRawLocked() error {
	if s.restoreRaw != nil || s.makeRaw == nil {
		return nil
	}
	if s.turnActive && !s.steering {
		// A turn without chat.steering must stay cooked so Ctrl-C becomes SIGINT;
		// with steering the turn reads raw input on purpose (SetBusy).
		return nil
	}
	restore, err := s.makeRaw()
	if err != nil {
		return fmt.Errorf("enable terminal input editing: %w", err)
	}
	s.restoreRaw = restore
	// Ask the terminal to bracket pastes for as long as a line is being edited
	// (SetBracketedPasteMode), so a pasted multi-line text reaches the editor as
	// one chunk with its newlines and does not submit at the first of them. A
	// terminal that does not support it is unaffected: the byte pump recognises
	// such a paste by the bytes that follow (input_pump.go).
	s.setBracketedPasteLocked(true)
	return nil
}

// leaveRaw restores the original (cooked) tty state; idempotent and safe from any goroutine.
func (s *chatInputSource) leaveRaw() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leaveRawLocked()
}

func (s *chatInputSource) leaveRawLocked() error {
	if s.restoreRaw == nil {
		return nil
	}
	// The line is done (or the terminal goes back to cooked mode for a turn): the
	// terminal must not keep bracketing pastes, whose markers would otherwise
	// reach a cooked-mode line read as bytes (SetBracketedPasteMode).
	s.setBracketedPasteLocked(false)
	err := s.restoreRaw()
	s.restoreRaw = nil
	return err
}

// setBracketedPasteLocked asks the terminal to bracket pastes (on) or to stop
// (off); without a terminal there is nothing to ask.
func (s *chatInputSource) setBracketedPasteLocked(on bool) {
	if s.terminal != nil {
		s.terminal.SetBracketedPasteMode(on)
	}
}

// run serves one read request after another, so no read is started before one was
// asked for (see request).
func (s *chatInputSource) run() {
	defer close(s.done)
	for {
		var request chatInputRequest
		select {
		case <-s.ctx.Done():
			return
		case request = <-s.requests:
		}
		s.serve(request)
	}
}

// serve reads for one request (a prompt line, or a steer read while a turn runs; an
// ask read is requested by Ask) and delivers what it read to the channel of the mode
// the read is in. A steer read also reads on for input a turn does not act on
// (Ctrl-D on an empty line), so the input line stays available for the whole turn;
// the turn consumer ends it by asking for the next prompt read. Every other read ends
// with the one event it produced.
func (s *chatInputSource) serve(request chatInputRequest) {
	for {
		line, err := s.readLine(request)
		s.byteConsumed() // the line editor is done with every byte it got
		if s.ctx.Err() != nil {
			return
		}
		mode := s.currentReadMode()
		event, ok := s.readResult(mode, line, err)
		if !ok {
			continue
		}
		if !s.deliver(mode, event) {
			return
		}
		if mode != modeSteer {
			return
		}
	}
}

func (s *chatInputSource) readLine(request chatInputRequest) (string, error) {
	prompt := request.prompt
	// An input that already ended answers its EOF at once (also for input that is not
	// a terminal, whose read would return it again anyway).
	if err := s.stickyEOF(); err != nil {
		return "", err
	}
	if s.terminal != nil {
		// Raw mode only while editing a line. Ctrl-C here arrives as byte 0x03 (no SIGINT) and the
		// line editor returns io.EOF, so the prompt exits chat like SIGINT did with cooked line reads.
		if err := s.enterRaw(); err != nil {
			return "", err
		}
		defer s.leaveRaw()
		s.terminal.SetPrompt(prompt)
		s.reader.activitySent = false
		var line string
		var err error
		switch s.currentReadMode() {
		case modeAsk:
			// The answer of a question stays out of history.
			line, err = s.terminal.ReadLineNoHistory()
		case modeSteer:
			// A turn reads its keys without an input line on the screen: the model's
			// own output must not be written around one (design M1).
			line, err = s.terminal.ReadLineNoEcho()
		default:
			// Give back a half-typed line that a question took out of an earlier read.
			if saved := s.takeSavedLine(); saved != "" {
				s.terminal.SetPendingLine(saved)
			}
			line, err = s.terminal.ReadLine()
		}
		return line, err
	}
	if _, err := fmt.Fprint(s.output, prompt); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(&chatActivityReader{source: s, input: s.input})
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			s.recordEOF(err)
			return "", err
		}
		s.recordEOF(io.EOF)
		return "", io.EOF
	}
	return scanner.Text(), nil
}

type chatTerminalIO struct {
	reader *chatActivityReader
	output io.Writer
}

func (rw *chatTerminalIO) Read(p []byte) (int, error) {
	return rw.reader.Read(p)
}

func (rw *chatTerminalIO) Write(p []byte) (int, error) {
	return rw.output.Write(p)
}

// lockedWriter serializes writes from the input goroutine (prompts) and the chat loop.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
