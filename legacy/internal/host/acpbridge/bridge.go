package acpbridge

import "context"

type Bridge interface {
	Dispatch(ctx context.Context, req DispatchRequest) (DispatchResult, error)
}
