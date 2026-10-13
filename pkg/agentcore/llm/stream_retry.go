package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// streamReadError marks a network error while reading a response body (the
// connection dropped mid-stream). It is the only failure retryStream retries:
// parse errors and API errors inside the stream are not transient.
type streamReadError struct{ err error }

func (e *streamReadError) Error() string { return e.err.Error() }
func (e *streamReadError) Unwrap() error { return e.err }

// retryStream runs one streamed request (attempt) and repeats it from the
// start when it fails with a streamReadError, up to the policy's attempts
// with b's waits. Each retry, including the HTTP retries inside attempt
// (postWithRetry), is announced to emit as a StreamEventRetry before the
// wait, so a frontend can tell the user why nothing happens. When the broken
// attempt already emitted text the event's metadata has restart=true: the
// next attempt streams the message again from its start. attempt receives
// the context carrying that notifier and emit wrapped to record text deltas.
func retryStream(ctx context.Context, b *backoff, provider string, emit agentcoreapi.StreamFunc,
	attempt func(ctx context.Context, emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error)) (*agentcoreapi.GenerateResult, error) {
	sent := false // the current attempt emitted text
	var tracked agentcoreapi.StreamFunc
	if emit != nil {
		tracked = func(ctx context.Context, ev agentcoreapi.StreamEvent) error {
			if ev.Kind == agentcoreapi.StreamEventTextDelta && ev.Delta != "" {
				sent = true
			}
			return emit(ctx, ev)
		}
		ctx = withRetryNotifier(ctx, func(ctx context.Context, attempt int, wait time.Duration, reason string) error {
			return emit(ctx, agentcoreapi.StreamEvent{Kind: agentcoreapi.StreamEventRetry, Delta: reason, Metadata: map[string]any{
				"attempt": attempt, "wait_ms": wait.Milliseconds(), "restart": sent,
			}})
		})
	}
	for n := 1; ; n++ {
		sent = false
		res, err := attempt(ctx, tracked)
		var readErr *streamReadError
		if err == nil || !errors.As(err, &readErr) || ctx.Err() != nil {
			return res, err
		}
		if n >= b.attempts() {
			return nil, fmt.Errorf("%s stream broke %d times: %w", provider, n, err)
		}
		wait := b.next(0)
		slog.Warn("llm stream broke, requesting again", "provider", provider, "attempt", n, "text_shown", sent, "wait", wait.Round(time.Millisecond), "err", err)
		if err := noteRetry(ctx, n, wait, "stream broken"); err != nil {
			return nil, err
		}
		if err := sleepRetry(ctx, wait); err != nil {
			return nil, err
		}
	}
}
