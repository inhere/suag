package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/inhere/suag/internal/personal/process"
	"golang.org/x/term"
)

type chatInputEvent struct {
	activity bool
	line     string
	err      error
	injected bool
}

type chatInputRequest struct{ prompt string }

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
	terminal   *term.Terminal
	requests   chan chatInputRequest
	events     chan chatInputEvent
	activity   chan struct{}
	deliveries chan chatInputEvent
	done       chan struct{}
	mu         sync.Mutex
	pending    bool
	busy       bool
	reader     *chatActivityReader
}

func newChatInputSource(parent context.Context, input io.Reader, output io.Writer) (*chatInputSource, error) {
	ctx, cancel := context.WithCancel(parent)
	source := &chatInputSource{
		ctx: ctx, cancel: cancel, input: input, output: &lockedWriter{w: output},
		requests: make(chan chatInputRequest), events: make(chan chatInputEvent, 1),
		activity: make(chan struct{}), deliveries: make(chan chatInputEvent, 8), done: make(chan struct{}),
	}
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		// Raw mode is entered per line in readLine, not here: during model/tool execution the
		// terminal must stay cooked so Ctrl-C raises SIGINT and stderr keeps CR translation.
		source.makeRaw = func() (func() error, error) {
			state, err := term.MakeRaw(int(file.Fd()))
			if err != nil {
				return nil, err
			}
			return func() error { return term.Restore(int(file.Fd()), state) }, nil
		}
		source.reader = &chatActivityReader{source: source, input: input}
		terminalIO := &chatTerminalIO{reader: source.reader, output: source.output}
		source.terminal = term.NewTerminal(terminalIO, "")
	}
	go source.run()
	return source, nil
}

func (s *chatInputSource) Deliver(text, from string) (bool, error) {
	if strings.TrimSpace(text) == "" {
		return false, fmt.Errorf("delivery text is empty")
	}
	from = safeSender(from)
	prefix := "[来自 " + from + "] "
	if strings.EqualFold(from, "gofer") {
		prefix = goferReplyPrefix
	}
	if strings.HasPrefix(text, goferReplyPrefix) || strings.HasPrefix(text, "[来自 ") {
		prefix = ""
	}
	queued := s.isBusy() || len(s.deliveries) > 0
	event := chatInputEvent{line: prefix + text, injected: true}
	select {
	case s.deliveries <- event:
		return queued, nil
	default:
		return true, process.ErrQueueFull
	}
}

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

func (s *chatInputSource) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// SetBusy marks a turn start/end. Starting a turn forces the tty back to cooked mode even when a
// line read begun earlier is still blocked (a gofer/send delivery can start a turn while the
// prompt is waiting); otherwise Ctrl-C would arrive as byte 0x03 and never interrupt the turn.
func (s *chatInputSource) SetBusy(busy bool) {
	s.mu.Lock()
	s.busy = busy
	if busy {
		s.turnActive = true
		_ = s.leaveRawLocked()
	}
	s.mu.Unlock()
}

func (s *chatInputSource) Request(prompt string) bool {
	s.mu.Lock()
	// Back at a prompt: raw mode is allowed again. If a read from before the turn is still
	// blocked it continues as this prompt's read, so switch to raw for it right here.
	s.turnActive = false
	if s.pending {
		err := s.enterRawLocked()
		s.mu.Unlock()
		_ = err // best effort: the pending read still works in cooked mode
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
	if s.restoreRaw != nil || s.turnActive || s.makeRaw == nil {
		return nil
	}
	restore, err := s.makeRaw()
	if err != nil {
		return fmt.Errorf("enable terminal input editing: %w", err)
	}
	s.restoreRaw = restore
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
	err := s.restoreRaw()
	s.restoreRaw = nil
	return err
}

func (s *chatInputSource) run() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case request := <-s.requests:
			line, err := s.readLine(request.prompt)
			event := chatInputEvent{line: line, err: err}
			select {
			case s.events <- event:
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *chatInputSource) readLine(prompt string) (string, error) {
	if s.terminal != nil {
		// Raw mode only while editing a line. Ctrl-C here arrives as byte 0x03 (no SIGINT) and the
		// line editor returns io.EOF, so the prompt exits chat like SIGINT did with cooked line reads.
		if err := s.enterRaw(); err != nil {
			return "", err
		}
		defer s.leaveRaw()
		s.terminal.SetPrompt(prompt)
		s.reader.activitySent = false
		line, err := s.terminal.ReadLine()
		if errors.Is(err, term.ErrPasteIndicator) {
			err = nil
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
			return "", err
		}
		return "", io.EOF
	}
	return scanner.Text(), nil
}

type chatActivityReader struct {
	source       *chatInputSource
	input        io.Reader
	activitySent bool
}

func (r *chatActivityReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var one [1]byte
	n, err := r.input.Read(one[:])
	if n == 0 {
		return 0, err
	}
	if !r.activitySent {
		r.activitySent = true
		select {
		case r.source.events <- chatInputEvent{activity: true}:
		case <-r.source.ctx.Done():
			return 0, r.source.ctx.Err()
		}
		select {
		case <-r.source.activity:
		case <-r.source.ctx.Done():
			return 0, r.source.ctx.Err()
		}
	}
	p[0] = one[0]
	return 1, nil
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
