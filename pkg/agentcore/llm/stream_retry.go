package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// streamReadError marks a stream that broke while being read: the connection
// dropped mid-stream, or the body ended in the middle of a line (see
// streamTruncatedError). It is the only failure retryStream retries: a parse
// error or an API error inside the stream is not transient - the next request
// would only get it again.
type streamReadError struct{ err error }

func (e *streamReadError) Error() string { return e.err.Error() }
func (e *streamReadError) Unwrap() error { return e.err }

// streamTruncatedError names a stream the provider did not finish: the
// connection (a proxy, gateway or load balancer in between included) closed
// while the answer was still streaming. detail says what was seen of it. The
// error wraps io.ErrUnexpectedEOF, so callers can tell a cut stream from a
// transport error; it is a streamReadError, so retryStream requests the whole
// answer again.
func streamTruncatedError(provider, detail string) error {
	return &streamReadError{fmt.Errorf("read %s stream: stream truncated, %s: %w", provider, detail, io.ErrUnexpectedEOF)}
}

// streamBodyError wraps the error of reading an event-stream body: a body that
// ended early (io.ErrUnexpectedEOF, which a dropped chunked connection reports
// too) is a truncated stream, any other read failure is reported as it is.
func streamBodyError(provider string, err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return streamTruncatedError(provider, "the connection closed before the answer was complete")
	}
	return &streamReadError{fmt.Errorf("read %s stream: %w", provider, err)}
}

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
