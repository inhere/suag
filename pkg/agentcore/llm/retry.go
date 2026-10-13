package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RetryPolicy is how a provider retries a model request that failed for a
// transient reason: a network error, HTTP 408/429/5xx, or a response stream
// that broke (llm.retry in the config). Waits double from BaseDelay up to
// MaxDelay with ±20% jitter; a Retry-After from the server is honoured. The
// wait level is shared by the provider's requests and falls back to
// BaseDelay after ResetAfter without a failure, so a flaky stretch backs off
// and a later blip is retried quickly again.
type RetryPolicy struct {
	// MaxAttempts is the attempts of one request, the first included.
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	ResetAfter  time.Duration
}

// DefaultRetryPolicy: up to 6 attempts, 1s doubling to 60s (about two
// minutes of waiting in all), reset after 2 minutes without failures.
var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 60 * time.Second, ResetAfter: 2 * time.Minute}

// withDefaults fills the zero fields from DefaultRetryPolicy.
func (p RetryPolicy) withDefaults() RetryPolicy {
	d := DefaultRetryPolicy
	if p.MaxAttempts > 0 {
		d.MaxAttempts = p.MaxAttempts
	}
	if p.BaseDelay > 0 {
		d.BaseDelay = p.BaseDelay
	}
	if p.MaxDelay > 0 {
		d.MaxDelay = p.MaxDelay
	}
	if p.ResetAfter > 0 {
		d.ResetAfter = p.ResetAfter
	}
	d.MaxDelay = max(d.MaxDelay, d.BaseDelay)
	return d
}

// backoff is a provider's retry state: the wait level grows with each
// failure and resets after ResetAfter without one.
type backoff struct {
	policy RetryPolicy
	mu     sync.Mutex
	level  int
	last   time.Time
	// now and jitter are replaced by tests.
	now    func() time.Time
	jitter func() float64 // in [-1, 1)
}

func newBackoff(p RetryPolicy) *backoff {
	return &backoff{policy: p.withDefaults(), now: time.Now, jitter: func() float64 { return rand.Float64()*2 - 1 }}
}

// attempts is the attempts allowed per request.
func (b *backoff) attempts() int { return b.policy.MaxAttempts }

// next records a failure and returns the wait before the next attempt;
// retryAfter (a server's Retry-After) raises it, capped at MaxDelay.
func (b *backoff) next(retryAfter time.Duration) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if !b.last.IsZero() && now.Sub(b.last) > b.policy.ResetAfter {
		b.level = 0
	}
	b.last = now
	d := b.policy.BaseDelay << min(b.level, 20)
	if d <= 0 || d > b.policy.MaxDelay {
		d = b.policy.MaxDelay
	}
	b.level++
	d += time.Duration(float64(d) * 0.2 * b.jitter())
	if retryAfter > d {
		d = retryAfter
	}
	return min(d, b.policy.MaxDelay)
}

// RetryReport collects the retries of one model call (GenerateObserved puts
// one in the context; the providers' retry loops add to it).
type RetryReport struct {
	Retries int
	Wait    time.Duration
	// Last is the reason of the latest retry (an HTTP status or an error).
	Last string
}

type retryReportKey struct{}

func withRetryReport(ctx context.Context) (context.Context, *RetryReport) {
	r := &RetryReport{}
	return context.WithValue(ctx, retryReportKey{}, r), r
}

// retryNotifier is told of each retry before its wait (retryStream turns it
// into a StreamEventRetry); attempt counts the failed attempts so far.
type retryNotifier func(ctx context.Context, attempt int, wait time.Duration, reason string) error

type retryNotifierKey struct{}

func withRetryNotifier(ctx context.Context, fn retryNotifier) context.Context {
	return context.WithValue(ctx, retryNotifierKey{}, fn)
}

// noteRetry records a retry in the context's RetryReport and tells its
// retryNotifier; a notifier error (the consumer gave up) stops the retries.
func noteRetry(ctx context.Context, attempt int, wait time.Duration, reason string) error {
	if r, _ := ctx.Value(retryReportKey{}).(*RetryReport); r != nil {
		r.Retries++
		r.Wait += wait
		r.Last = reason
	}
	if fn, _ := ctx.Value(retryNotifierKey{}).(retryNotifier); fn != nil {
		return fn(ctx, attempt, wait, reason)
	}
	return nil
}

// sleepRetry waits d unless ctx ends first.
func sleepRetry(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryableStatus reports the HTTP statuses worth another attempt.
func retryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}

// retryAfterOf reads a Retry-After header (seconds or an HTTP date).
func retryAfterOf(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// postWithRetry sends the request build makes until a response comes back
// that is not worth retrying (2xx, or a 4xx other than 408/429), retrying
// network errors and 408/429/5xx with b's waits. The caller reads and closes
// the returned response. A retried response's body is logged and dropped.
func postWithRetry(ctx context.Context, client *http.Client, b *backoff, provider, path string,
	build func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := 1; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		var retryAfter time.Duration
		var reason string
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr, reason = err, "network error"
		case retryableStatus(resp.StatusCode):
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			logProviderResponse(provider, path, resp.StatusCode, body)
			retryAfter = retryAfterOf(resp.Header, time.Now())
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			reason = fmt.Sprintf("HTTP %d", resp.StatusCode)
		default:
			return resp, nil
		}
		if attempt >= b.attempts() {
			return nil, fmt.Errorf("%s request failed after %d attempts: %w", provider, attempt, lastErr)
		}
		wait := b.next(retryAfter)
		slog.Warn("llm request failed, retrying", "provider", provider, "attempt", attempt, "reason", reason, "wait", wait.Round(time.Millisecond), "err", lastErr)
		if err := noteRetry(ctx, attempt, wait, reason); err != nil {
			return nil, err
		}
		if err := sleepRetry(ctx, wait); err != nil {
			return nil, err
		}
	}
}
