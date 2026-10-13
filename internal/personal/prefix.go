package personal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"

	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/llm"
)

// Request prefix outcomes recorded on model events (attr "prefix"): how the
// request relates to the previous one of the session, which decides how much
// of it a provider prompt cache can serve.
const (
	prefixFirst   = "first"   // no earlier request in this process
	prefixAppend  = "append"  // the previous request is a prefix of this one
	prefixFold    = "fold"    // an earlier message got shorter (history folding)
	prefixSegment = "segment" // a new segment replaced the history
	prefixSystem  = "system"  // the system prompt changed
	prefixTools   = "tools"   // the tool set changed
	prefixRewrite = "rewrite" // any other change to an earlier message
)

// prefixReport splits a request into the part an unchanged prefix could serve
// from the provider cache (reused), the part that had been sent before but
// changed position or form (rebuilt: re-sent after the break) and what is new
// since the previous request. Sizes are JSON characters, a proxy for tokens.
type prefixReport struct {
	Reason                 string
	Reused, Rebuilt, Fresh int
}

// requestPrint is what is kept of a request to compare the next one with.
type requestPrint struct {
	system, tools         [32]byte
	systemSize, toolsSize int
	msgs                  []messagePrint
}

// messagePrint: sum is cumulative (this message and all before it), id names
// the message (role and tool call), size is its JSON length.
type messagePrint struct {
	sum     [32]byte
	id      string
	size    int
	segment bool
}

// prefixTracker compares each model request with the previous one of its
// session. A BeforeModel hook feeds it; the model event recorder reads the
// report of the request in flight.
type prefixTracker struct {
	mu      sync.Mutex
	last    map[string]*requestPrint
	reports map[string]prefixReport
}

func newPrefixTracker() *prefixTracker {
	return &prefixTracker{last: map[string]*requestPrint{}, reports: map[string]prefixReport{}}
}

// observe fingerprints req, compares it with the session's previous request
// and keeps the report for the model event.
func (t *prefixTracker) observe(sessionID string, req *agentcore.GenerateRequest) {
	if t == nil || sessionID == "" || req == nil {
		return
	}
	cur := printRequest(req)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reports[sessionID] = comparePrints(t.last[sessionID], cur)
	t.last[sessionID] = cur
}

// report returns the report of the session's request in flight.
func (t *prefixTracker) report(sessionID string) (prefixReport, bool) {
	if t == nil {
		return prefixReport{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.reports[sessionID]
	return r, ok
}

func printRequest(req *agentcore.GenerateRequest) *requestPrint {
	p := &requestPrint{msgs: make([]messagePrint, 0, len(req.Messages))}
	sys := []byte(req.System)
	p.system, p.systemSize = sha256.Sum256(sys), len(sys)
	tools, _ := json.Marshal(req.Tools)
	p.tools, p.toolsSize = sha256.Sum256(tools), len(tools)
	var sum [32]byte
	for _, m := range req.Messages {
		raw, _ := json.Marshal(m)
		h := sha256.New()
		h.Write(sum[:])
		h.Write(raw)
		copy(sum[:], h.Sum(nil))
		id := m.Role
		switch {
		case m.ToolResult != nil:
			id += ":" + m.ToolResult.CallID
		case m.ToolCall != nil:
			id += ":" + m.ToolCall.CallID
		}
		segment, _ := m.Metadata[llm.SegmentContextMetaKey].(bool)
		p.msgs = append(p.msgs, messagePrint{sum: sum, id: id, size: len(raw), segment: segment})
	}
	return p
}

// comparePrints finds where cur leaves prev and why.
func comparePrints(prev, cur *requestPrint) prefixReport {
	sizeOf := func(msgs []messagePrint) int {
		n := 0
		for _, m := range msgs {
			n += m.size
		}
		return n
	}
	head := cur.systemSize + cur.toolsSize
	if prev == nil {
		return prefixReport{Reason: prefixFirst, Fresh: head + sizeOf(cur.msgs)}
	}
	k := 0 // messages in the common prefix
	for k < len(prev.msgs) && k < len(cur.msgs) && prev.msgs[k].sum == cur.msgs[k].sum {
		k++
	}
	// Messages of cur at positions prev also had are "rebuilt" after a break.
	common := min(len(prev.msgs), len(cur.msgs))
	r := prefixReport{Fresh: sizeOf(cur.msgs[common:])}
	switch {
	case prev.system != cur.system || prev.tools != cur.tools:
		r.Reason = prefixTools
		if prev.system != cur.system {
			r.Reason = prefixSystem
		}
		r.Rebuilt = head + sizeOf(cur.msgs[:common])
		return r
	case k == len(prev.msgs):
		r.Reason, r.Reused = prefixAppend, head+sizeOf(cur.msgs[:k])
		return r
	}
	r.Reused, r.Rebuilt = head+sizeOf(cur.msgs[:k]), sizeOf(cur.msgs[k:common])
	switch {
	case k >= len(cur.msgs):
		r.Reason = prefixRewrite // cur is a shortened prev
	case cur.msgs[k].segment:
		r.Reason = prefixSegment
	case cur.msgs[k].id == prev.msgs[k].id && cur.msgs[k].size < prev.msgs[k].size:
		r.Reason = prefixFold
	default:
		r.Reason = prefixRewrite
	}
	return r
}

// prefixHook feeds the tracker with every request the runtime sends.
type prefixHook struct {
	agentcore.NopHook
	tracker *prefixTracker
}

func (h prefixHook) BeforeModel(ctx context.Context, event *agentcore.ModelEvent, _ *agentcore.Control) error {
	if event != nil {
		h.tracker.observe(observeScopeFrom(ctx).sessionID, event.Request)
	}
	return nil
}
