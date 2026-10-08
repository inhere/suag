package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

type chatInputEvent struct {
	activity bool
	line     string
	err      error
}

type chatInputRequest struct{ prompt string }

// chatInputSource reads exactly one prompt per request and signals its first byte before
// forwarding it to the line editor, so callers can interrupt a waiting Stop hook first.
type chatInputSource struct {
	ctx      context.Context
	cancel   context.CancelFunc
	input    io.Reader
	output   io.Writer
	ttyFile  *os.File
	state    *term.State
	terminal *term.Terminal
	requests chan chatInputRequest
	events   chan chatInputEvent
	activity chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	pending  bool
	closeErr error
	reader   *chatActivityReader
}

func newChatInputSource(parent context.Context, input io.Reader, output io.Writer) (*chatInputSource, error) {
	ctx, cancel := context.WithCancel(parent)
	source := &chatInputSource{
		ctx: ctx, cancel: cancel, input: input, output: output,
		requests: make(chan chatInputRequest), events: make(chan chatInputEvent, 1),
		activity: make(chan struct{}), done: make(chan struct{}),
	}
	if file, ok := input.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		state, err := term.MakeRaw(int(file.Fd()))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("enable terminal input editing: %w", err)
		}
		source.ttyFile, source.state = file, state
		source.reader = &chatActivityReader{source: source, input: input}
		terminalIO := &chatTerminalIO{reader: source.reader, output: output}
		source.terminal = term.NewTerminal(terminalIO, "")
	}
	go source.run()
	return source, nil
}

func (s *chatInputSource) Request(prompt string) bool {
	s.mu.Lock()
	if s.pending {
		s.mu.Unlock()
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
	if s.ttyFile != nil && s.state != nil {
		s.closeErr = term.Restore(int(s.ttyFile.Fd()), s.state)
	}
	return s.closeErr
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
