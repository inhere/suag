package personal

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

// Steering and a segment break: a steer is an entry injected into the running
// turn, so a mid-turn break cuts and keeps it like every other item of the view
// (suga-0lxy), it goes into the handoff package in its own section, not among
// the turn's requests (design D10), and the summary input labels it.

// longJobPrompt is the request of the long turn below (kept short so the user
// request section of a package is easy to assert).
const longJobPrompt = "run the long job"

// compactWithSteerRunner runs the long turn the break tests share: 20 tool calls
// whose fifth result is huge enough to cross the threshold, and one steer from
// the tool call numbered steerAt (a person typing into the running turn).
func compactWithSteerRunner(t *testing.T, steerAt int, steer Steer) (*scriptProv, *store.Store, RunResult) {
	t.Helper()
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 20 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	var runner *Runner
	const sid = "s-steer-compact"
	reg := sizedToolRegistry(100, map[int]int{5: 15000}, func(i int) {
		if i != steerAt {
			return
		}
		accepted, err := runner.Steer(sid, steer)
		assert.NoErr(t, err)
		assert.Eq(t, true, accepted)
	})
	runner = compactRunner(t, repo, prov, reg, func(c *Config) {
		c.InlineOutputBytes = 1 << 20 // keep the huge result in the context
	})
	_, err := runner.NewSession(context.Background(), sid, "compaction with a steer")
	assert.Require(t, assert.NoErr(t, err))
	res, err := runner.Run(context.Background(), RunRequest{SessionID: sid, Prompt: longJobPrompt})
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, "done", res.Output)
	assert.Require(t, assert.Eq(t, 1, len(prov.sumReqs))) // the break summarized, no truncation fallback
	assert.Empty(t, res.ReturnedSteers)                  // the steer was delivered
	return prov, repo, res
}

// A steer taken before the cut point leaves the raw context with the messages
// before it and is summarized; the package still names it in its own section.
func TestSteerBeforeTheCutGoesIntoTheSummary(t *testing.T) {
	steer := Steer{ID: "s1", Text: "only change the README", Source: steerSourceKeyboard}
	prov, repo, res := compactWithSteerRunner(t, 1, steer)

	// The break keeps the last call/result group (the huge result included):
	// nothing before it, and so no steer, stays as a message of the turn. The
	// steer's text only shows in the segment context message, i.e. in the
	// package's "本回合插话" section.
	view := firstSegmentView(t, prov, "SUMMARY-main-1")
	assert.Eq(t, 0, occurrences(view[1:], steer.Text))
	assert.Eq(t, 1, occurrences(view, steer.Text))
	assertWellFormed(t, view)
	// The summarizer reads it as a steering message, not as a user request.
	assert.StrContains(t, prov.sumReqs[0].Messages[0].Content, "[插话 keyboard] "+steer.Text)

	records := compactionEntries(t, repo, res.SessionID)
	assert.Require(t, assert.Len(t, records, 1))
	assert.Eq(t, "SUMMARY-main-1\n\n## 最近的用户请求（原文）\n1. "+longJobPrompt+
		"\n\n## 本回合插话（原文）\n1. "+steer.Text+"（来源：keyboard）", records[0].Summary)
	assert.Eq(t, []string{longJobPrompt}, records[0].RecentRequests)
}

// A steer taken in the step that breaks is the last item of the view: the break
// cuts right before it and keeps it verbatim as the first item of the new
// segment, with everything before it (the huge result included) summarized.
func TestSteerAtTheCutIsKeptVerbatim(t *testing.T) {
	steer := Steer{ID: "s2", Text: "wait, check the config first", Source: "send:alice"}
	prov, repo, res := compactWithSteerRunner(t, 5, steer)

	view := firstSegmentView(t, prov, "SUMMARY-main-1")
	assert.Require(t, assert.Eq(t, 2, len(view))) // the segment context and the steer
	assert.Eq(t, true, isSteerItem(view[1]))
	assert.Eq(t, true, isInjectedItem(view[1]))
	assert.Eq(t, steer.Text, view[1].Content)
	assert.Eq(t, "user", view[1].Role)
	// Kept once as a message of the turn; the second occurrence is the package's
	// "本回合插话" section in the segment context message.
	assert.Eq(t, 1, occurrences(view[1:], steer.Text))
	assert.Eq(t, 2, occurrences(view, steer.Text))
	assertWellFormed(t, view)
	// Everything before it left the raw context and went into the summary input.
	sum := prov.sumReqs[0].Messages[0].Content
	assert.StrContains(t, sum, strings.Repeat("x", 200))
	assert.StrContains(t, sum, longJobPrompt)

	records := compactionEntries(t, repo, res.SessionID)
	assert.Require(t, assert.Len(t, records, 1))
	assert.StrContains(t, records[0].Summary, "## 本回合插话（原文）\n1. "+steer.Text+"（来源：send:alice）")
	assert.Eq(t, []string{longJobPrompt}, records[0].RecentRequests)
}
