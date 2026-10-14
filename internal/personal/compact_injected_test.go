package personal

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
	"github.com/inhere/suga/pkg/agentcore/tasks"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// sizedToolRegistry answers each call with content of the size its number maps
// to (defaultSize otherwise), notifying onCall with the call number.
func sizedToolRegistry(defaultSize int, sizes map[int]int, onCall func(int)) agentcore.ToolRegistry {
	reg := tools.NewRegistry()
	var mu sync.Mutex
	n := 0
	reg.Register(fnTool{name: "big", fn: func() (*agentcore.ToolResult, error) {
		mu.Lock()
		n++
		i := n
		mu.Unlock()
		if onCall != nil {
			onCall(i)
		}
		size := defaultSize
		if s, ok := sizes[i]; ok {
			size = s
		}
		return &agentcore.ToolResult{Name: "big", Content: strings.Repeat("x", size)}, nil
	}})
	return reg
}

// firstSegmentView returns the messages of the first model request showing the
// summary as its segment context message.
func firstSegmentView(t *testing.T, prov *scriptProv, summary string) []agentcore.Message {
	t.Helper()
	for _, req := range prov.reqs {
		if len(req.Messages) > 0 && isSegmentContext(req.Messages[0]) && strings.Contains(req.Messages[0].Content, summary) {
			return req.Messages
		}
	}
	t.Fatalf("no request shows a segment context with %q", summary)
	return nil
}

// occurrences counts the messages holding text.
func occurrences(msgs []agentcore.Message, text string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.Content, text) {
			n++
		}
	}
	return n
}

// compactWithNoticeRunner is the long turn the two notice tests share: 20 tool
// calls whose fifth result is huge enough to cross the threshold, one task
// notice taken while a tool call runs.
func compactWithNoticeRunner(t *testing.T, notices *fakeNotices, noticeAt int, ended tasks.Info) (*Runner, *scriptProv, *store.Store, RunResult) {
	t.Helper()
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 20 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	reg := sizedToolRegistry(100, map[int]int{5: 15000}, func(i int) {
		if i == noticeAt { // the task ends while this call runs
			notices.set(ended)
		}
	})
	runner := compactRunner(t, repo, prov, reg, func(c *Config) {
		c.Tasks = notices
		c.InlineOutputBytes = 1 << 20 // keep the huge result in the context
	})
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "run the long job"})
	assert.NoErr(t, err)
	assert.Eq(t, "done", res.Output)
	assert.Require(t, assert.True(t, len(prov.sumReqs) >= 1)) // the break summarized, no truncation fallback
	records := compactionEntries(t, repo, res.SessionID)
	assert.Require(t, assert.True(t, len(records) >= 1))
	assert.Eq(t, segmentReasonThreshold, records[0].Reason)
	return runner, prov, repo, res
}

func testEndedTask() tasks.Info {
	return tasks.Info{ID: "t1", Kind: "shell", Summary: "go test ./...", Status: tasks.Exited, ExitCode: 1,
		Started: time.Now().Add(-time.Minute), Ended: time.Now(), Lines: 2, Ref: "out:s:ab",
		Tail: []string{"--- FAIL: TestX", "FAIL"}}
}

// The notice taken while the last tool call runs is the closest legal cut
// point: the threshold break keeps it verbatim as the first item of the new
// segment, and everything before it goes into the summary (issue suga-0lxy:
// this used to fail with "turn ... has N stored messages but N-1 in memory").
func TestCompactMidTurnKeepsTaskNoticeAtTheCut(t *testing.T) {
	notices := &fakeNotices{}
	_, prov, _, _ := compactWithNoticeRunner(t, notices, 5, testEndedTask())

	view := firstSegmentView(t, prov, "SUMMARY-main-1")
	assert.Require(t, assert.Eq(t, 2, len(view)))
	assert.Eq(t, true, isInjectedItem(view[1])) // right after the segment context
	assert.StrContains(t, view[1].Content, "[background task notice]")
	assert.StrContains(t, view[1].Content, "t1 exited (exit 1)")
	assert.Eq(t, 1, occurrences(view, "[background task notice]")) // kept once, not replayed
	assertWellFormed(t, view)
	// Everything before the notice (the huge result included) left the raw
	// context: it went into the summary input instead.
	assert.Eq(t, -1, indexOf(view, strings.Repeat("x", 200)))
	sum := prov.sumReqs[0].Messages[0].Content
	assert.StrContains(t, sum, strings.Repeat("x", 200))
	assert.StrContains(t, sum, "run the long job")
}

// A notice kept by a break must stay a view item: the next break of the same
// turn has to summarize it instead of dropping it unnoticed.
func TestCompactMidTurnKeepsNoticeAcrossTwoCuts(t *testing.T) {
	repo := openObserveRepo(t)
	notices := &fakeNotices{}
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		if n <= 20 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("done")
	}}
	// The first huge result crosses the threshold in the step the notice of a
	// task ended during that call arrives (the break keeps it); the second one
	// triggers the next break, which cuts the notice away.
	reg := sizedToolRegistry(100, map[int]int{5: 15000, 15: 15000}, func(i int) {
		if i == 5 {
			notices.set(testEndedTask())
		}
	})
	runner := compactRunner(t, repo, prov, reg, func(c *Config) {
		c.Tasks = notices
		c.InlineOutputBytes = 1 << 20
	})
	res, err := runner.Run(context.Background(), RunRequest{Prompt: "run the long job"})
	assert.NoErr(t, err)
	assert.Eq(t, "done", res.Output)
	assert.Require(t, assert.Eq(t, 2, len(prov.sumReqs)))
	assert.Require(t, assert.Eq(t, 2, len(firstSegmentView(t, prov, "SUMMARY-main-1")))) // [segment context, notice]

	// The second break summarized everything before it, the kept notice included.
	sum := prov.sumReqs[1].Messages[0].Content
	assert.StrContains(t, sum, "[background task notice]")
	assert.StrContains(t, sum, "t1 exited (exit 1)")
	// It is gone from the view, and the view keeps the last call/result group.
	view := prov.reqs[len(prov.reqs)-1].Messages
	assert.Eq(t, 0, occurrences(view, "[background task notice]"))
	assert.StrContains(t, view[0].Content, "SUMMARY-main-2")
	assertWellFormed(t, view)
}

// A notice the cut leaves behind goes into the summary input instead of being
// dropped silently, and it never counts as a user request of the turn.
func TestCompactMidTurnSummarizesTaskNoticeBeforeTheCut(t *testing.T) {
	notices := &fakeNotices{}
	_, prov, repo, res := compactWithNoticeRunner(t, notices, 1, testEndedTask())

	// The summarizer sees the notice ...
	sum := prov.sumReqs[0].Messages[0].Content
	assert.StrContains(t, sum, "[background task notice]")
	assert.StrContains(t, sum, "t1 exited (exit 1)")
	// ... the kept view does not (it was summarized, not lost): the break keeps
	// the pending call/result group and drops the notice with the messages
	// before it, without shifting the kept messages.
	view := firstSegmentView(t, prov, "SUMMARY-main-1")
	assert.Eq(t, 0, occurrences(view, "[background task notice]"))
	assert.Require(t, assert.True(t, len(view) >= 3))
	assert.Eq(t, "assistant", view[1].Role)
	assert.Eq(t, true, view[1].ToolCall != nil)
	assert.Eq(t, "tool", view[2].Role)
	assert.Eq(t, view[1].ToolCall.CallID, view[2].ToolCall.CallID)
	assertWellFormed(t, view)
	// The notice is a stored entry of the turn, not a user request.
	records := compactionEntries(t, repo, res.SessionID)
	assert.Eq(t, []string{"run the long job"}, records[0].RecentRequests)
}
