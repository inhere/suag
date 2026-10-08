package llm

import (
	"context"
	"time"

	agentcoreapi "github.com/inhere/suag/pkg/agentcore/api"
)

// Attempt describes one call to one provider, including fallback attempts made
// by a Pool. It is delivered to an AttemptObserver after the call returns.
type Attempt struct {
	Provider  string
	Model     string // empty when the provider does not expose Model()
	Index     int    // 1-based attempt number within one logical request
	Streamed  bool
	StartedAt time.Time
	Duration  time.Duration
	// TTFT is the time to the first streamed text delta; zero for non-streaming
	// calls and for streams that produced no delta.
	TTFT  time.Duration
	Usage agentcoreapi.Usage
	Err   error
}

// AttemptObserver receives every provider attempt. It must not block.
type AttemptObserver func(ctx context.Context, a Attempt)

// modelNamer is optionally implemented by providers that know their model.
type modelNamer interface{ Model() string }

func providerModel(p agentcoreapi.Provider) string {
	if m, ok := p.(modelNamer); ok {
		return m.Model()
	}
	return ""
}

// GenerateObserved calls prov.Generate and reports the attempt to obs (may be nil).
func GenerateObserved(ctx context.Context, prov agentcoreapi.Provider, req *agentcoreapi.GenerateRequest, index int, obs AttemptObserver) (*agentcoreapi.GenerateResult, error) {
	start := time.Now()
	res, err := prov.Generate(ctx, req)
	report(ctx, obs, prov, index, false, start, 0, res, err)
	return res, err
}

// StreamObserved calls prov.Stream, measuring time-to-first-delta, and reports
// the attempt to obs (may be nil). The emit callback is passed through unchanged.
func StreamObserved(ctx context.Context, prov agentcoreapi.StreamingProvider, req *agentcoreapi.GenerateRequest, emit agentcoreapi.StreamFunc, index int, obs AttemptObserver) (*agentcoreapi.GenerateResult, error) {
	start := time.Now()
	var ttft time.Duration
	wrapped := emit
	if obs != nil {
		wrapped = func(ctx context.Context, ev agentcoreapi.StreamEvent) error {
			if ttft == 0 && ev.Kind == agentcoreapi.StreamEventTextDelta && ev.Delta != "" {
				ttft = max(time.Since(start), time.Nanosecond)
			}
			if emit == nil {
				return nil
			}
			return emit(ctx, ev)
		}
	}
	res, err := prov.Stream(ctx, req, wrapped)
	report(ctx, obs, prov, index, true, start, ttft, res, err)
	return res, err
}

func report(ctx context.Context, obs AttemptObserver, prov agentcoreapi.Provider, index int, streamed bool, start time.Time, ttft time.Duration, res *agentcoreapi.GenerateResult, err error) {
	if obs == nil {
		return
	}
	a := Attempt{Provider: prov.Name(), Model: providerModel(prov), Index: index, Streamed: streamed,
		StartedAt: start, Duration: time.Since(start), TTFT: ttft, Err: err}
	if res != nil {
		a.Usage = res.Usage
	}
	obs(ctx, a)
}
