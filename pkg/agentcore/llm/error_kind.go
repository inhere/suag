package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Error kinds a host reports for a turn that failed on a provider error.
const (
	ErrorKindTransient   = "provider_transient"
	ErrorKindBadResponse = "provider_bad_response"
	ErrorKindRejected    = "provider_rejected"
	// ErrorKindQuota means the provider account or billing allowance is exhausted.
	// It is checked before transient because weekly quota responses are often
	// labelled rate_limit_error/RATE_LIMITED by APIs.
	ErrorKindQuota = "provider_quota"
)

type badResponseError struct{ err error }

func (e *badResponseError) Error() string { return e.err.Error() }
func (e *badResponseError) Unwrap() error { return e.err }
func badResponsef(format string, args ...any) error {
	return &badResponseError{fmt.Errorf(format, args...)}
}

type transientRequestError struct{ err error }

func (e *transientRequestError) Error() string { return e.err.Error() }
func (e *transientRequestError) Unwrap() error { return e.err }

type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }
func (e *statusError) Status() int   { return e.status }

// quotaError preserves an optional RFC3339 reset time for trace and hosts.
type quotaError struct {
	err     error
	resetAt string
}

func (e *quotaError) Error() string { return e.err.Error() }
func (e *quotaError) Unwrap() error { return e.err }

// ResetAt returns the provider-supplied quota reset time, if one was present.
func ResetAt(err error) string {
	var q *quotaError
	if errors.As(err, &q) {
		return q.resetAt
	}
	return ""
}

func statusErr(status int, err error) error {
	if err == nil || status >= 200 && status < 300 {
		return err
	}
	return &statusError{status: status, err: err}
}

// ErrorKind classifies provider failures; unrelated cancellation, storage,
// tool and hook failures return an empty kind.
func ErrorKind(err error) string {
	if isQuotaError(err) {
		return ErrorKindQuota
	}
	var readErr *streamReadError
	if errors.As(err, &readErr) {
		return ErrorKindTransient
	}
	var transient *transientRequestError
	if errors.As(err, &transient) {
		return ErrorKindTransient
	}
	var rejected *statusError
	if errors.As(err, &rejected) {
		return ErrorKindRejected
	}
	var bad *badResponseError
	if errors.As(err, &bad) {
		return ErrorKindBadResponse
	}
	return ""
}

// ShouldFallback reports whether another provider may succeed. Quota is allowed
// here, while Pool separately prevents it from selecting another model of the
// same provider (the account allowance would be shared).
func ShouldFallback(err error) bool {
	switch ErrorKind(err) {
	case ErrorKindTransient, ErrorKindQuota:
		return true
	case ErrorKindRejected:
		var rejected *statusError
		if errors.As(err, &rejected) {
			return rejected.Status() == http.StatusUnauthorized || rejected.Status() == http.StatusForbidden || rejected.Status() == http.StatusNotFound
		}
	}
	return false
}

func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	var status *statusError
	if errors.As(err, &status) && status.Status() == http.StatusPaymentRequired {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"insufficient_quota", "billing_hard_limit_reached", "usage limit", "quota", "limit resets", "billing"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func fallbackReason(err error) string {
	reason := ErrorKind(err)
	if reason == "" {
		reason = "unclassified"
	}
	var rejected *statusError
	if errors.As(err, &rejected) {
		reason = fmt.Sprintf("%s (HTTP %d)", reason, rejected.Status())
	}
	return reason
}

func apiErrorMessage(code, message string) string {
	if code == "" {
		return message
	}
	return code + ": " + message
}
