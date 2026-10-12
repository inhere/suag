package llm

import (
	"context"
	"errors"
	"log/slog"
	"time"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// streamReadRetries is how many times a response that broke while its body was
// being read is requested again.
const streamReadRetries = 2

// streamRetryDelay is the wait before the n-th retry (n from 1); a var so tests
// can shorten it.
var streamRetryDelay = func(n int) time.Duration { return time.Duration(n) * time.Second }

// streamReadError marks a network error while reading a response body (the
// connection dropped mid-stream). It is the only failure retryStream retries:
// parse errors and API errors inside the stream are not transient.
type streamReadError struct{ err error }

func (e *streamReadError) Error() string { return e.err.Error() }
func (e *streamReadError) Unwrap() error { return e.err }

// retryStream runs one streamed request (attempt) and repeats it from the
// start when it fails with a streamReadError before any text delta was
// emitted. Until then everything the stream produced (reasoning, tool call
// fragments, usage) was only accumulated, so a retry is invisible to the
// frontend. After a text delta the error is returned as is: repeating would
// show the text twice. attempt receives emit wrapped to record text deltas.
func retryStream(ctx context.Context, provider string, emit agentcoreapi.StreamFunc,
	attempt func(emit agentcoreapi.StreamFunc) (*agentcoreapi.GenerateResult, error)) (*agentcoreapi.GenerateResult, error) {
	for n := 0; ; n++ {
		sent := false
		var tracked agentcoreapi.StreamFunc
		if emit != nil {
			tracked = func(ctx context.Context, ev agentcoreapi.StreamEvent) error {
				if ev.Kind == agentcoreapi.StreamEventTextDelta && ev.Delta != "" {
					sent = true
				}
				return emit(ctx, ev)
			}
		}
		res, err := attempt(tracked)
		var readErr *streamReadError
		if err == nil || !errors.As(err, &readErr) || sent || n >= streamReadRetries || ctx.Err() != nil {
			return res, err
		}
		slog.Warn("llm stream broke before any text was shown, requesting again", "provider", provider, "attempt", n+1, "err", err)
		select {
		case <-time.After(streamRetryDelay(n + 1)):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
