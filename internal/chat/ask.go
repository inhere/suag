package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/inhere/suga/pkg/agentcore/tools"
)

// errAskCancelled is returned when the person ends an approval question with
// Ctrl-C or Ctrl-D. It wraps context.Canceled so the runner records the turn as
// cancelled; the approval layer wraps it in tools.ErrAborted.
var errAskCancelled = fmt.Errorf("确认被取消: %w", context.Canceled)

// askPollInterval is how often Ask re-checks a pending read that is between
// states (just starting, or its finished line not yet in events).
const askPollInterval = 2 * time.Millisecond

// chatAsker binds the chat terminal to the approval layer (tools.Asker).
type chatAsker struct {
	source *chatInputSource
	render *Renderer
}

func (a chatAsker) Ask(ctx context.Context, q tools.Question) (string, error) {
	return a.source.Ask(ctx, a.render.ApprovalDetails(q), a.render.ApprovalPrompt(q))
}

// Ask shows detail and reads one answer line on prompt, in the middle of a
// turn. Nothing typed before the question can become the answer:
//
//  1. Raw mode for the question (turnActive is cleared meanwhile); afterwards
//     turnActive and cooked mode are restored, so Ctrl-C in a tool that runs
//     next raises SIGINT again.
//  2. Unread terminal input is flushed; a byte the reader already holds is
//     dropped (inputEpoch) and one already passed to the editor is waited for.
//  3. A read started before the turn may still be pending:
//     - its line already finished (in events, or on the way): that line is
//     kept as the next input after the turn and a new read asks the question;
//     - still editing: the half-typed line is taken out (prefilled into the
//     next prompt read) and the same read answers the question.
//  4. activity events are acknowledged; the answer stays out of history;
//     deliveries keep queueing (they are never read here).
//
// Ctrl-C/Ctrl-D returns errAskCancelled. Without a terminal it returns
// tools.ErrCannotAsk.
func (s *chatInputSource) Ask(ctx context.Context, detail, prompt string) (string, error) {
	if s.terminal == nil {
		return "", tools.ErrCannotAsk
	}
	s.askMu.Lock()
	defer s.askMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", tools.ErrCannotAsk
	}
	s.turnActive = false
	if err := s.enterRawLocked(); err != nil {
		s.turnActive = true
		s.mu.Unlock()
		return "", err
	}
	if s.flushInput != nil {
		_ = s.flushInput() // best effort: the epoch below still drops held bytes
	}
	s.inputEpoch++
	for s.handing {
		s.cond.Wait()
	}
	pending := s.pending
	s.mu.Unlock()

	var held *chatInputEvent
	answerRead := false // an answer read is outstanding (its event not consumed)
	defer func() { s.endAsk(held, answerRead) }()

	reuse := false
	if pending {
		var err error
		if held, reuse, err = s.settlePendingRead(ctx); err != nil {
			return "", err
		}
	}

	if _, err := s.Write([]byte(detail)); err != nil {
		return "", err
	}
	answerRead = true
	if reuse {
		if err := s.terminal.UpdatePrompt(prompt); err != nil {
			return "", err
		}
	} else {
		s.mu.Lock()
		s.pending = true
		s.mu.Unlock()
		select {
		case s.requests <- chatInputRequest{prompt: prompt, ask: true}:
		case <-ctx.Done():
			s.CompleteLine() // the request was never taken
			answerRead = false
			return "", ctx.Err()
		case <-s.ctx.Done():
			return "", s.ctx.Err()
		}
	}
	for {
		select {
		case event := <-s.events:
			if event.activity {
				s.AckActivity()
				continue
			}
			s.CompleteLine()
			answerRead = false
			if event.err != nil {
				s.terminal.AbortLine()
				return "", errAskCancelled
			}
			return event.line, nil
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.ctx.Done():
			return "", s.ctx.Err()
		}
	}
}

// settlePendingRead finds out what the read started before the turn is doing.
// It returns the finished line to keep (held), or reuse=true when the read is
// still editing and will answer the question (its half line is saved).
func (s *chatInputSource) settlePendingRead(ctx context.Context) (held *chatInputEvent, reuse bool, err error) {
	// take handles one event: activity is acknowledged (its held byte is
	// dropped by the epoch); anything else is the finished line.
	take := func(event chatInputEvent) *chatInputEvent {
		if event.activity {
			s.AckActivity()
			return nil
		}
		return &event
	}
	for {
		select {
		case event := <-s.events:
			if held = take(event); held != nil {
				return held, false, nil
			}
			continue
		default:
		}
		line, reading := s.terminal.TakeLine()
		s.addSavedLine(line)
		if reading {
			return nil, true, nil
		}
		// Not editing: the read is just starting, or its line is on the way.
		select {
		case event := <-s.events:
			if held = take(event); held != nil {
				return held, false, nil
			}
		case <-time.After(askPollInterval):
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-s.ctx.Done():
			return nil, false, s.ctx.Err()
		}
	}
}

// endAsk restores the turn state: cooked mode and turnActive, and a line that
// finished before the question goes back to events as the next input (unless
// an answer read is still outstanding, which only happens when the turn is
// being cancelled).
func (s *chatInputSource) endAsk(held *chatInputEvent, answerRead bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnActive = true
	_ = s.leaveRawLocked()
	if held != nil && !answerRead {
		s.pending = true
		select {
		case s.events <- *held:
		default:
		}
	}
}

func (s *chatInputSource) addSavedLine(line string) {
	if line == "" {
		return
	}
	s.mu.Lock()
	s.savedLine += line
	s.mu.Unlock()
}

func (s *chatInputSource) takeSavedLine() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved := s.savedLine
	s.savedLine = ""
	return saved
}

func (s *chatInputSource) currentEpoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inputEpoch
}

// handOver marks a byte as passed to the line editor, unless the input was
// flushed after it was read (then it must be dropped and false is returned).
func (s *chatInputSource) handOver(epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inputEpoch != epoch {
		return false
	}
	s.handing = true
	return true
}

// byteConsumed records that the line editor processed the last byte it got.
func (s *chatInputSource) byteConsumed() {
	s.mu.Lock()
	if s.handing {
		s.handing = false
		s.cond.Broadcast()
	}
	s.mu.Unlock()
}
