package llm

import (
	"errors"
	"fmt"
)

// Error kinds a host reports for a turn that failed on a provider error, the
// values of ErrorKind. They travel as "error_kind" on the trace turn event and
// on the run's stream-json error line, so a caller such as gofer can tell a cut
// answer apart from an unusable one when it decides whether to continue the
// session.
const (
	// ErrorKindTransient: the stream broke or was cut off and its retries ran
	// out (streamReadError). The request itself was fine, so a later attempt or
	// a continued session is likely to work; the text of the cut answer was
	// already shown and is void (retryStream restarted the message, see the
	// stream-json "retry" lines with restart=true).
	ErrorKindTransient = "provider_transient"
	// ErrorKindBadResponse: the provider answered, but with something unusable -
	// a body or stream event that is not valid JSON, an answer that is missing
	// its content, or an error the API reported itself. Requesting it again
	// would get the same answer, so it is not retried.
	ErrorKindBadResponse = "provider_bad_response"
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

// ErrorKind classifies a failed model request for the hosts: ErrorKindTransient
// (a broken or cut stream whose retries ran out), ErrorKindBadResponse (a
// provider answer that cannot be used, or an API error of its own) or "" for
// everything else - a cancelled turn, a storage, tool or hook failure, or an
// error that did not come from a provider. The value goes on the turn event's
// attrs and on the stream-json error line.
func ErrorKind(err error) string {
	var readErr *streamReadError
	if errors.As(err, &readErr) {
		return ErrorKindTransient
	}
	var bad *badResponseError
	if errors.As(err, &bad) {
		return ErrorKindBadResponse
	}
	return ""
}
