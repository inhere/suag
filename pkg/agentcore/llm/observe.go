package llm

import (
	"context"
	"time"

	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
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
	// ToolCalls, ReasoningChars and TextChars describe the response: how
	// many tool calls it issued (more than one = a parallel step) and how
	// long its reasoning and text were. Zero when the attempt failed.
	ToolCalls, ReasoningChars, TextChars int
	// Finish is how the response ended: "tool_calls", "final", or
	// "truncated" when the output limit cut it off; "" when it failed.
	Finish string
	// Retries is how often the provider retried the request after a
	// transient failure (RetryPolicy), RetryWait the time spent waiting and
	// RetryReason the reason of the last retry ("HTTP 503", "stream broken").
	Retries     int
	RetryWait   time.Duration
	RetryReason string
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
	ctx, retries := withRetryReport(ctx)
	res, err := prov.Generate(ctx, req)
	report(ctx, obs, prov, index, false, start, 0, retries, res, err)
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
	ctx, retries := withRetryReport(ctx)
	res, err := prov.Stream(ctx, req, wrapped)
	report(ctx, obs, prov, index, true, start, ttft, retries, res, err)
	return res, err
}

func report(ctx context.Context, obs AttemptObserver, prov agentcoreapi.Provider, index int, streamed bool, start time.Time, ttft time.Duration,
	retries *RetryReport, res *agentcoreapi.GenerateResult, err error) {
	if obs == nil {
		return
	}
	a := Attempt{Provider: prov.Name(), Model: providerModel(prov), Index: index, Streamed: streamed,
		StartedAt: start, Duration: time.Since(start), TTFT: ttft, Err: err,
		Retries: retries.Retries, RetryWait: retries.Wait, RetryReason: retries.Last}
	if res != nil {
		a.Usage = res.Usage
		a.ToolCalls, a.ReasoningChars, a.TextChars = len(res.ToolCalls), len(res.Reasoning), len(res.Content)
		a.Finish = finishOf(res)
	}
	obs(ctx, a)
}

// finishOf names how a response ended (Attempt.Finish).
func finishOf(res *agentcoreapi.GenerateResult) string {
	for _, c := range res.ToolCalls {
		if c.ArgumentsError == truncatedCallError {
			return "truncated"
		}
	}
	if res.FinishReason == agentcoreapi.FinishToolCalls {
		return "tool_calls"
	}
	return "final"
}
