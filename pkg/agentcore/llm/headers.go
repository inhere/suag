package llm

import (
	"context"
	"net/http"
	"strings"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

// DefaultUserAgent identifies suga to model APIs; llm.headers can override it.
const DefaultUserAgent = "suga"

// SessionIDPlaceholder in an llm.headers value is replaced with the current
// suga session id, e.g. a gateway's per-conversation routing header.
const SessionIDPlaceholder = "{{session_id}}"

// WithSessionID marks ctx with the suga session the model call belongs to.
//
// Deprecated: use agentcoreapi.WithSessionID. The session id belongs to the
// runtime contract, not to the provider layer (the tasks manager reads it
// too), so the key moved to api; this wrapper forwards to it and stays for
// existing embedders.
func WithSessionID(ctx context.Context, id string) context.Context {
	return agentcoreapi.WithSessionID(ctx, id)
}

// SessionIDFrom returns the session id set by WithSessionID, or "".
//
// Deprecated: use agentcoreapi.SessionIDFrom.
func SessionIDFrom(ctx context.Context) string { return agentcoreapi.SessionIDFrom(ctx) }

// applyHeaders sets the default User-Agent and then the configured extra
// headers. A value using SessionIDPlaceholder is skipped when ctx carries no
// session id, so a gateway never receives an empty conversation id.
func applyHeaders(ctx context.Context, req *http.Request, headers map[string]string) {
	req.Header.Set("User-Agent", DefaultUserAgent)
	sessionID := agentcoreapi.SessionIDFrom(ctx)
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
