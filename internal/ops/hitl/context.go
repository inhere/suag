package hitl

import "context"

type ticketContextKey struct{}

// TicketContext carries host runtime identifiers so HITL tickets can be
// correlated back to the current session/turn/run in the workbench.
type TicketContext struct {
	SessionID string
	TurnID    string
	RunID     string
}

func WithTicketContext(ctx context.Context, ticketCtx TicketContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ticketContextKey{}, ticketCtx)
}

func TicketContextFromContext(ctx context.Context) (TicketContext, bool) {
	if ctx == nil {
		return TicketContext{}, false
	}
	value, ok := ctx.Value(ticketContextKey{}).(TicketContext)
	if !ok {
		return TicketContext{}, false
	}
	return value, true
}
