package personal

import "github.com/inhere/suag/pkg/agentcore"

// Price is a model price in US dollars per million tokens.
type Price struct {
	Input, Output, CacheRead, CacheWrite float64
}

// Cost prices u. It uses the same normalization as ReportUsage (provider
// InputTokens includes cache read/write, so those are subtracted from the
// plain input and billed at their own rates).
func (p Price) Cost(u agentcore.Usage) float64 {
	r := ReportUsage(u)
	return (float64(r.InputTokens)*p.Input + float64(r.OutputTokens)*p.Output +
		float64(r.CacheReadTokens)*p.CacheRead + float64(r.CacheWriteTokens)*p.CacheWrite) / 1e6
}

// costOf returns the cost of one model call: the provider-reported cost wins;
// otherwise it is computed from the configured price (0 when there is none).
// price may be nil: personal never reads global configuration itself.
func costOf(price func(provider, model string) (Price, bool), provider, model string, u agentcore.Usage) float64 {
	if u.CostUSD != 0 {
		return u.CostUSD
	}
	if price == nil {
		return 0
	}
	if p, ok := price(provider, model); ok {
		return p.Cost(u)
	}
	return 0
}
