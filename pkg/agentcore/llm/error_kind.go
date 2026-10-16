package llm

import (
	"errors"
	"fmt"
	"net/http"
)

// Error kinds a host reports for a turn that failed on a provider error, the
// values of ErrorKind. They travel as "error_kind" on the trace turn event, on
// the run's stream-json error line and in an ACP prompt failure, so a caller
// such as gofer can tell a cut answer apart from an unusable one when it
// decides whether to continue the session. The pool uses them too, to decide
// whether another provider is worth trying (ShouldFallback).
const (
	// ErrorKindTransient: the request itself was fine and the provider failed
	// on something that usually passes on a new attempt - a network error, one
	// of the retryable statuses (HTTP 408/429/5xx) or the response header
	// timeout, whose retries ran out (transientRequestError), or a stream that
	// broke or was cut off (streamReadError). Either way a later attempt, or
	// another provider, is likely to work; the text of a cut answer was already
	// shown and is void (retryStream restarted the message, see the stream-json
	// "retry" lines with restart=true).
	ErrorKindTransient = "provider_transient"
	// ErrorKindBadResponse: the provider answered, but with something unusable -
	// a body or stream event that is not valid JSON, an answer that is missing
	// its content, or an error the API reported itself. Requesting it again
	// would get the same answer, so it is not retried.
	ErrorKindBadResponse = "provider_bad_response"
	// ErrorKindRejected: the provider refused the request itself with an HTTP
	// 4xx that a retry at the same provider would only repeat: 400 (the request
	// is malformed for this API), 401/403 (the key is missing, wrong or not
	// allowed to use that model), 404 (the model does not exist) or 422 (the
	// arguments are unacceptable). The status says whether another provider is
	// worth trying: 400/422 describe the request and fail everywhere, while
	// 401/403/404 name this provider's credentials or models - see
	// ShouldFallback.
	ErrorKindRejected = "provider_rejected"
)

// badResponseError marks a provider answer that could not be used (see
// ErrorKind). It is transparent otherwise: the wrapped error keeps its text and
// its cause.
type badResponseError struct{ err error }

func (e *badResponseError) Error() string { return e.err.Error() }
func (e *badResponseError) Unwrap() error { return e.err }

// badResponsef builds a badResponseError from a format string; the providers
// use it for every answer they cannot use.
func badResponsef(format string, args ...any) error {
	return &badResponseError{fmt.Errorf(format, args...)}
}

// transientRequestError marks an HTTP request that kept failing transiently - a
// network error, a retryable status (408/429/5xx) or the response header
// timeout - until postWithRetry used up its attempts. It is transparent
// otherwise, and unlike a streamReadError nothing was emitted yet; both mean
// the same to a host (ErrorKind) and to the pool (ShouldFallback): the request
// was fine, so trying again, or asking another provider, is worthwhile.
type transientRequestError struct{ err error }

func (e *transientRequestError) Error() string { return e.err.Error() }
func (e *transientRequestError) Unwrap() error { return e.err }

// statusError wraps the error of a response a provider refused with a non-2xx
// status (see statusErr). The status is what tells a request-level rejection
// (400/422) from one that names the provider (401/403/404), so it is kept
// beside the message, which stays the wrapped error's.
type statusError struct {
	status int
	err    error
}

func (e *statusError) Error() string { return e.err.Error() }
func (e *statusError) Unwrap() error { return e.err }

// Status is the HTTP status of the response the error came from.
func (e *statusError) Status() int { return e.status }

// statusErr marks err as the error of a non-2xx response; err is returned
// unchanged for a 2xx, since a gateway may answer 200 with an error body - that
// is a bad response, not a rejection.
func statusErr(status int, err error) error {
	if err == nil || status >= 200 && status < 300 {
		return err
	}
	return &statusError{status: status, err: err}
}

// ErrorKind classifies a failed model request for the hosts: ErrorKindTransient
// (a request or stream that failed transiently and whose retries ran out),
// ErrorKindRejected (the provider refused the request with a 4xx),
// ErrorKindBadResponse (a provider answer that cannot be used, or an API error
// of its own) or "" for everything else - a cancelled turn, a storage, tool or
// hook failure, or an error that did not come from a provider. The value goes
// on the turn event's attrs, on the stream-json error line and into an ACP
// prompt failure's data.
func ErrorKind(err error) string {
	var readErr *streamReadError
	if errors.As(err, &readErr) {
		return ErrorKindTransient
	}
	var transient *transientRequestError
	if errors.As(err, &transient) {
		return ErrorKindTransient
	}
	// A rejection is checked before a bad response: the provider's own message
	// travels inside it, but the status is the more precise statement.
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

// ShouldFallback reports whether asking the next provider the same request could
// still succeed, which is what a Pool (main model plus fallback_models) asks
// before it burns the remaining models on an error:
//
//   - a transient failure (broken stream, network error, 408/429/5xx, response
//     header timeout) says nothing about the model that was asked, so another
//     one may well answer;
//   - a rejection of 401, 403 or 404 names this provider - its credentials or
//     its model list - and says nothing about the next one;
//   - everything else would be repeated by the next provider just the same: a
//     400/422 rejection describes the request itself, a bad response or an
//     unclassified error is returned as it is.
func ShouldFallback(err error) bool {
	switch ErrorKind(err) {
	case ErrorKindTransient:
		return true
	case ErrorKindRejected:
		var rejected *statusError
		if errors.As(err, &rejected) {
			return rejected.Status() == http.StatusUnauthorized ||
				rejected.Status() == http.StatusForbidden ||
				rejected.Status() == http.StatusNotFound
		}
	}
	return false
}

// fallbackReason names a failed attempt for the pool's log line and for the
// trace record of what it did with the failure (Attempt.Fallback): the error
// kind and, when the provider refused the request, the HTTP status.
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
