package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RetryPolicy struct {
	MaxAttempts                     int
	BaseDelay, MaxDelay, ResetAfter time.Duration
}

var DefaultRetryPolicy = RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 60 * time.Second, ResetAfter: 10 * time.Minute}

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

type backoff struct {
	policy RetryPolicy
	mu     sync.Mutex
	level  int
	last   time.Time
	now    func() time.Time
	jitter func() float64
}

func newBackoff(p RetryPolicy) *backoff {
	return &backoff{policy: p.withDefaults(), now: time.Now, jitter: func() float64 { return rand.Float64()*2 - 1 }}
}
func (b *backoff) attempts() int { return b.policy.MaxAttempts }
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
	d += time.Duration(float64(d) * .2 * b.jitter())
	if retryAfter > d {
		d = retryAfter
	}
	return min(d, b.policy.MaxDelay)
}

type RetryReport struct {
	Retries int
	Wait    time.Duration
	Last    string
}
type retryReportKey struct{}

func withRetryReport(ctx context.Context) (context.Context, *RetryReport) {
	r := &RetryReport{}
	return context.WithValue(ctx, retryReportKey{}, r), r
}

type retryNotifier func(context.Context, int, time.Duration, string) error
type retryNotifierKey struct{}

func withRetryNotifier(ctx context.Context, fn retryNotifier) context.Context {
	return context.WithValue(ctx, retryNotifierKey{}, fn)
}
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
func retryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}
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

var resetAtPattern = regexp.MustCompile(`(?i)resets at\s+([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.+\-]+Z)`)

func quotaResetAt(text string) string {
	m := resetAtPattern.FindStringSubmatch(text)
	if len(m) == 2 {
		if _, err := time.Parse(time.RFC3339Nano, m[1]); err == nil {
			return m[1]
		}
	}
	return ""
}
func quotaText(status int, text string) bool {
	if status == http.StatusPaymentRequired {
		return true
	}
	lower := strings.ToLower(text)
	for _, m := range []string{"insufficient_quota", "billing_hard_limit_reached", "usage limit", "quota", "limit resets", "billing"} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
func quotaFailure(status int, provider string, body []byte) error {
	msg := fmt.Sprintf("%s request failed with HTTP %d: %s", provider, status, strings.TrimSpace(string(body)))
	if reset := quotaResetAt(string(body)); reset != "" {
		msg += fmt.Sprintf(" (resets at %s)", reset)
		return &quotaError{err: statusErr(status, fmt.Errorf("%s", msg)), resetAt: reset}
	}
	return &quotaError{err: statusErr(status, fmt.Errorf("%s", msg))}
}

// postWithRetry retries network and transient HTTP failures. Quota responses
// are recognized from the body before the normal 429 retry path and never wait.
func postWithRetry(ctx context.Context, client *http.Client, b *backoff, provider, path string, build func() (*http.Request, error)) (*http.Response, error) {
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
			if ErrorKind(err) == ErrorKindRejected {
				// Redirect rejection is a policy failure, not a transient network error.
				return nil, err
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr, reason = err, "network error"
		case retryableStatus(resp.StatusCode):
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			logProviderResponse(provider, path, resp.StatusCode, body)
			if quotaText(resp.StatusCode, string(body)) {
				return nil, quotaFailure(resp.StatusCode, provider, body)
			}
			retryAfter = retryAfterOf(resp.Header, time.Now())
			lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			reason = fmt.Sprintf("HTTP %d", resp.StatusCode)
		default:
			return resp, nil
		}
		if attempt >= b.attempts() {
			return nil, &transientRequestError{fmt.Errorf("%s request failed after %d attempts: %w", provider, attempt, lastErr)}
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
