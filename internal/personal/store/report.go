package store

import (
	"context"
	"math"
	"sort"
)

// ModelStat aggregates kind=model events of one provider/model pair.
// InputTokens follows the REPORTING convention (cache excluded, see
// personal.ReportUsage): stored input minus cache read/write, floored at 0 per
// event. CacheHitRate = cache_read / (input + cache_read + cache_write).
type ModelStat struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Calls        int     `json:"calls"`
	Errors       int     `json:"errors"`
	ErrorRate    float64 `json:"error_rate"`
	P50Ms        int64   `json:"p50_ms"`
	P95Ms        int64   `json:"p95_ms"`
	P50TTFTMs    int64   `json:"p50_ttft_ms,omitempty"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheRead    int64   `json:"cache_read_tokens"`
	CacheWrite   int64   `json:"cache_write_tokens"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	CostUSD      float64 `json:"cost_usd"`
}

// ToolStat aggregates tool events by tool name. Calls counts executed tool
// events; denial events (which replace the tool event when approval refuses a
// call) are counted in Denials only.
type ToolStat struct {
	Name      string  `json:"name"`
	Calls     int     `json:"calls"`
	Errors    int     `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	Denials   int     `json:"denials"`
	P50Ms     int64   `json:"p50_ms"`
	P95Ms     int64   `json:"p95_ms"`
}

// Report is the result of `suag stats`.
type Report struct {
	Models      []ModelStat `json:"models"`
	Tools       []ToolStat  `json:"tools"`
	Compactions int         `json:"compactions"`
	ToolSearch  int         `json:"tool_searches"`
}

// BuildReport queries events matching f (session/since) and aggregates them.
func (s *Store) BuildReport(ctx context.Context, f EventFilter) (Report, error) {
	f.Kinds = []string{EventModel, EventTool, EventDenial, EventCompaction, EventToolSearch}
	events, err := s.ListEvents(ctx, f)
	if err != nil {
		return Report{}, err
	}
	return AggregateEvents(events), nil
}

// AggregateEvents is the pure aggregation behind BuildReport.
func AggregateEvents(events []Event) Report {
	type modelAcc struct {
		stat       ModelStat
		durs, ttft []int64
	}
	type toolAcc struct {
		stat ToolStat
		durs []int64
	}
	models := map[[2]string]*modelAcc{}
	tools := map[string]*toolAcc{}
	tool := func(name string) *toolAcc {
		if tools[name] == nil {
			tools[name] = &toolAcc{stat: ToolStat{Name: name}}
		}
		return tools[name]
	}
	var rep Report
	for _, e := range events {
		switch e.Kind {
		case EventModel:
			model, _ := e.Attrs["model"].(string)
			key := [2]string{e.Name, model}
			a := models[key]
			if a == nil {
				a = &modelAcc{stat: ModelStat{Provider: e.Name, Model: model}}
				models[key] = a
			}
			a.stat.Calls++
			if e.Status == "error" {
				a.stat.Errors++
			}
			a.durs = append(a.durs, e.DurationMs)
			if e.TTFTMs > 0 {
				a.ttft = append(a.ttft, e.TTFTMs)
			}
			// Stored input includes cache; report it without (floored at 0).
			a.stat.InputTokens += max(e.InputTokens-e.CacheReadTokens-e.CacheWriteTokens, 0)
			a.stat.OutputTokens += e.OutputTokens
			a.stat.CacheRead += e.CacheReadTokens
			a.stat.CacheWrite += e.CacheWriteTokens
			if c, ok := e.Attrs["cost_usd"].(float64); ok {
				a.stat.CostUSD += c
			}
		case EventTool:
			a := tool(e.Name)
			a.stat.Calls++
			if e.Status == "error" {
				a.stat.Errors++
			}
			a.durs = append(a.durs, e.DurationMs)
		case EventDenial:
			tool(e.Name).stat.Denials++
		case EventCompaction:
			rep.Compactions++
		case EventToolSearch:
			rep.ToolSearch++
		}
	}
	rep.Models = make([]ModelStat, 0, len(models))
	for _, a := range models {
		st := a.stat
		st.ErrorRate = ratio(int64(st.Errors), int64(st.Calls))
		st.P50Ms, st.P95Ms = Percentile(a.durs, 50), Percentile(a.durs, 95)
		st.P50TTFTMs = Percentile(a.ttft, 50)
		st.CacheHitRate = ratio(st.CacheRead, st.InputTokens+st.CacheRead+st.CacheWrite)
		rep.Models = append(rep.Models, st)
	}
	sort.Slice(rep.Models, func(i, j int) bool {
		if rep.Models[i].Calls != rep.Models[j].Calls {
			return rep.Models[i].Calls > rep.Models[j].Calls
		}
		return rep.Models[i].Provider+"/"+rep.Models[i].Model < rep.Models[j].Provider+"/"+rep.Models[j].Model
	})
	rep.Tools = make([]ToolStat, 0, len(tools))
	for _, a := range tools {
		st := a.stat
		st.ErrorRate = ratio(int64(st.Errors), int64(st.Calls))
		st.P50Ms, st.P95Ms = Percentile(a.durs, 50), Percentile(a.durs, 95)
		rep.Tools = append(rep.Tools, st)
	}
	sort.Slice(rep.Tools, func(i, j int) bool {
		if rep.Tools[i].Calls != rep.Tools[j].Calls {
			return rep.Tools[i].Calls > rep.Tools[j].Calls
		}
		return rep.Tools[i].Name < rep.Tools[j].Name
	})
	return rep
}

func ratio(n, d int64) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Percentile returns the p-th percentile (0<p<=100) of vals by the
// nearest-rank method: the value at 1-based rank ceil(p/100*n) of the sorted
// data. Empty input yields 0. vals is not modified.
func Percentile(vals []int64, p float64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]int64(nil), vals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return sorted[rank-1]
}
