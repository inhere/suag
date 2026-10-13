package llm

import (
	"context"
	"net/http"
	"strings"
)

// DefaultUserAgent identifies suga to model APIs; llm.headers can override it.
const DefaultUserAgent = "suga"

// SessionIDPlaceholder in an llm.headers value is replaced with the current
// suga session id, e.g. a gateway's per-conversation routing header.
const SessionIDPlaceholder = "{{session_id}}"

type sessionIDKey struct{}

// WithSessionID marks ctx with the suga session the model call belongs to.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, id)
}

// SessionIDFrom returns the session id set by WithSessionID, or "".
func SessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}

// applyHeaders sets the default User-Agent and then the configured extra
// headers. A value using SessionIDPlaceholder is skipped when ctx carries no
// session id, so a gateway never receives an empty conversation id.
func applyHeaders(ctx context.Context, req *http.Request, headers map[string]string) {
	req.Header.Set("User-Agent", DefaultUserAgent)
	sessionID := SessionIDFrom(ctx)
	for name, value := range headers {
		if strings.Contains(value, SessionIDPlaceholder) {
			if sessionID == "" {
				continue
			}
			value = strings.ReplaceAll(value, SessionIDPlaceholder, sessionID)
		}
		req.Header.Set(name, value)
	}
}
