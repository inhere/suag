package personal

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools/native"
)

// PlanRecorder implements native.PlanSink. The update_plan tool is built before
// the Runner exists, so the recorder is bound to the Runner's store by
// Config.Plans in New (late binding). It persists a plan entry for the current
// turn and publishes a "plan" event through the running turn's Emit callback.
type PlanRecorder struct {
	mu   sync.Mutex
	repo *store.Store
}

// NewPlanRecorder creates an unbound recorder; pass it as Config.Plans.
func NewPlanRecorder() *PlanRecorder { return &PlanRecorder{} }

var _ native.PlanSink = (*PlanRecorder)(nil)

func (p *PlanRecorder) bind(repo *store.Store) {
	p.mu.Lock()
	p.repo = repo
	p.mu.Unlock()
}

// planEmitKey carries the current turn's event emitter in ctx.
type planEmitKey struct{}

type planEmitter func(ctx context.Context, plan native.Plan) error

func withPlanEmitter(ctx context.Context, fn planEmitter) context.Context {
	return context.WithValue(ctx, planEmitKey{}, fn)
}

// RecordPlan writes the plan entry (TurnID = current turn) and emits the event.
func (p *PlanRecorder) RecordPlan(ctx context.Context, plan native.Plan) error {
	p.mu.Lock()
	repo := p.repo
	p.mu.Unlock()
	scope := observeScopeFrom(ctx)
	if repo == nil || scope.sessionID == "" {
		return errors.New("plan recorder is not bound to a running turn")
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	if _, err := repo.AppendEntries(ctx, scope.sessionID, store.NewEntry{TurnID: scope.turnID, Type: store.EntryPlan, JSON: data}); err != nil {
		return err
	}
	if fn, ok := ctx.Value(planEmitKey{}).(planEmitter); ok && fn != nil {
		return fn(ctx, plan)
	}
	return nil
}
