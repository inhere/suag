package personal

import "errors"

// SessionNotFoundError reports that --resume or the --from source names no
// session. It is a stable contract for callers: exit code 4 and a stderr line
// starting with "session not found: ", so a caller can tell a missing (e.g.
// pre-store) session from any other failure and fall back to --from.
type SessionNotFoundError struct{ ID string }

func (e SessionNotFoundError) Error() string { return "session not found: " + e.ID }

// ExitCode is the process exit code main uses for this error.
func (e SessionNotFoundError) ExitCode() int { return 4 }

func (e SessionNotFoundError) Unwrap() error { return ErrSessionNotFound }

// SessionNotFound maps a not-found error for id to SessionNotFoundError.
func SessionNotFound(err error, id string) error {
	if errors.Is(err, ErrSessionNotFound) {
		return SessionNotFoundError{ID: id}
	}
	return err
}
