package personal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore"
)

// candidateLine matches one offered candidate in a summary request.
var candidateLine = regexp.MustCompile(`(?m)^- (\S+) — `)

// refPickProv answers summary requests with a package whose refs section
// lists the first offered output ref and message ref plus two bogus tokens
// (a path and a malformed output ref) that parseRefs must drop.
type refPickProv struct {
	*scriptProv
}

func (p refPickProv) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	res, err := p.scriptProv.Generate(ctx, req)
	if err != nil || req.System != compactSystemPrompt {
		return res, err
	}
	var out, msg string
	for _, m := range candidateLine.FindAllStringSubmatch(req.Messages[0].Content, -1) {
		switch {
		case strings.HasPrefix(m[1], outputRefPrefix) && out == "":
			out = m[1]
		case !strings.HasPrefix(m[1], outputRefPrefix) && msg == "":
			msg = m[1]
		}
	}
	res.Content += "\n## 当前任务\n修解析器\n" + refsHeading + "（ref + 一句话说明）\n- " + out + " 大日志\n- `" + msg +
		"` 用户请求\n- docs/12 只是路径\n- out:x:NOTHEX\n## 下一步\n无"
	return res, nil
}

// latestSegment decodes the newest segment record of a session.
func latestSegment(t *testing.T, repo *store.Store, sid string) compactionRecord {
	t.Helper()
	recs := segmentRecords(t, repo, sid)
	assert.Require(t, assert.True(t, len(recs) > 0))
	return recs[len(recs)-1]
}

// The handoff package carries the recent requests verbatim (filler and
// injected ones skipped, injected ones filling up when typed ones run out)
// and the refs the model picked from the offered candidates.
func TestHandoffPackageRequestsAndRefs(t *testing.T) {
	repo := openObserveRepo(t)
	sp := &scriptProv{name: "main", normal: func(n int, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "user" && strings.HasPrefix(last.Content, "read the big log") {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, refPickProv{sp}, bigToolRegistry(2000, nil), func(c *Config) {
		c.ContextWindow, c.CompactReserveTokens = 100000, 1000
		c.Spiller, c.InlineOutputBytes = NewSessionSpiller(t.TempDir()), 500
	})
	ctx := context.Background()
	sid := ""
	run := func(prompt string, injected bool) {
		t.Helper()
		res, err := runner.Run(ctx, RunRequest{SessionID: sid, Prompt: prompt, Injected: injected})
		assert.NoErr(t, err)
		sid = res.SessionID
	}
	run("read the big log please", false)
	run("继续", false)
	run("please fix the parser bug", true) // e.g. a Stop hook reply
	run("look at the failing test output", false)
	run("go on", false)
	run("continue", false)

	t.Run("injected marker is persisted", func(t *testing.T) {
		es, err := repo.ListEntries(ctx, sid, store.EntryFilter{Types: []string{store.EntryUser}})
		assert.NoErr(t, err)
		marked := map[string]bool{}
		for _, e := range es {
			var m agentcore.Message
			assert.NoErr(t, json.Unmarshal(e.JSON, &m))
			v, _ := m.Metadata[metaInjected].(bool)
			marked[m.Content] = v
		}
		assert.Eq(t, map[string]bool{"read the big log please": false, "继续": false, "please fix the parser bug": true,
			"look at the failing test output": false, "go on": false, "continue": false}, marked)
		// The turn itself sent the request unchanged (Metadata never reaches
		// the wire; only the persisted copy is tagged).
		third := sp.reqs[3].Messages
		assert.Eq(t, "please fix the parser bug", third[len(third)-1].Content)
		assert.Nil(t, third[len(third)-1].Metadata)
	})

	_, err := runner.Compact(ctx, sid)
	assert.NoErr(t, err)
	rec := latestSegment(t, repo, sid)
	t.Run("too few typed requests: an injected one fills up", func(t *testing.T) {
		assert.Eq(t, []string{"read the big log please", "please fix the parser bug", "look at the failing test output"}, rec.RecentRequests)
		assert.StrContains(t, rec.Summary, recentRequestsHeading+"\n1. read the big log please\n2. please fix the parser bug\n3. look at the failing test output")
		// The summarizer saw them too, and the candidates.
		sum := sp.sumReqs[0].Messages[0].Content
		assert.StrContains(t, sum, "# 最近的用户请求")
		assert.StrContains(t, sum, "# 候选引用")
	})
	t.Run("refs: picked candidates only, format-checked", func(t *testing.T) {
		assert.Require(t, assert.Eq(t, 2, len(rec.Refs)))
		_, _, err := parseOutputRef(rec.Refs[0])
		assert.NoErr(t, err)
		msgSID, _, err := parseMessageRef(rec.Refs[1])
		assert.NoErr(t, err)
		assert.Eq(t, sid, msgSID)
		// The ref resolves to the stored output.
		assert.StrContains(t, rec.Summary, rec.Refs[0])
	})

	run("now update the changelog entry", false)
	_, err = runner.Compact(ctx, sid)
	assert.NoErr(t, err)
	t.Run("enough typed requests: injected skipped", func(t *testing.T) {
		rec := latestSegment(t, repo, sid)
		assert.Eq(t, []string{"read the big log please", "look at the failing test output", "now update the changelog entry"}, rec.RecentRequests)
		// The next request renders the new package as the segment context.
		run("and then?", false)
		msgs := sp.reqs[len(sp.reqs)-1].Messages
		assert.Eq(t, true, isSegmentContext(msgs[0]))
		assert.StrContains(t, msgs[0].Content, "3. now update the changelog entry")
	})
}

// A threshold break mid-turn: the next model request starts with the segment
// context (package with the current request verbatim), followed only by the
// last complete call/result group.
func TestThresholdBreakNextRequest(t *testing.T) {
	repo := openObserveRepo(t)
	var prov *scriptProv
	var after *agentcore.GenerateRequest
	prov = &scriptProv{name: "main", normal: func(n int, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		prov.mu.Lock()
		broke := len(prov.sumReqs) > 0
		prov.mu.Unlock()
		if broke && after == nil {
			after = req
		}
		last := req.Messages[len(req.Messages)-1]
		if n == 1 || (last.Role == "user" && !strings.HasPrefix(last.Content, "do the long job")) {
			return finalText("first answer")
		}
		if n <= 12 {
			return callTool(fmt.Sprintf("c%d", n))
		}
		return finalText("long job done")
	}}
	runner := compactRunner(t, repo, prov, bigToolRegistry(2000, nil), nil)
	ctx := context.Background()
	first, err := runner.Run(ctx, RunRequest{Prompt: "first question about the parser"})
	assert.NoErr(t, err)
	res, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "do the long job carefully"})
	assert.NoErr(t, err)
	assert.Eq(t, "long job done", res.Output)
	assert.Require(t, assert.NotNil(t, after))

	msgs := after.Messages
	assert.Require(t, assert.Eq(t, 3, len(msgs)))
	assert.Eq(t, true, isSegmentContext(msgs[0]))
	assert.StrContains(t, msgs[0].Content, "SUMMARY-main-1")
	// The turn in progress is not adopted yet, its request counts anyway.
	assert.StrContains(t, msgs[0].Content, recentRequestsHeading+"\n1. first question about the parser\n2. do the long job carefully")
	assert.Eq(t, "assistant", msgs[1].Role)
	assert.Eq(t, true, msgs[1].ToolCall != nil)
	assert.Eq(t, "tool", msgs[2].Role)
	assert.Eq(t, msgs[1].ToolCall.CallID, msgs[2].ToolCall.CallID)

	recs := compactionEntries(t, repo, res.SessionID)
	assert.Require(t, assert.True(t, len(recs) >= 1))
	assert.Eq(t, segmentReasonThreshold, recs[0].Reason)
	assert.Eq(t, []string{"first question about the parser", "do the long job carefully"}, recs[0].RecentRequests)
	t.Run("a later break folds the package in", func(t *testing.T) {
		if len(prov.sumReqs) < 2 {
			t.Skip("only one break")
		}
		assert.StrContains(t, previousPackage(prov.sumReqs[1]), recentRequestsHeading)
	})
}

func TestFillerRequests(t *testing.T) {
	for text, want := range map[string]bool{
		"继续": true, "继续吧": true, "go on": true, "continue": true, "Continue.": true, "ok": true, "好的。": true,
		"keep going!": true, "fix the bug": false, "继续修复解析器的问题": false, "continue with step 3": false,
	} {
		t.Run(text, func(t *testing.T) {
			assert.Eq(t, want, isFillerRequest(text))
		})
	}
}

func TestParseRefs(t *testing.T) {
	const out = "out:sess_a:0123456789abcdef"
	body := "## 涉及文件\n- internal/x.go sess_a/9\n" + refsHeading + "\n- " + out + " 日志\n- sess_a/12 请求\n- other/7 候选\n" +
		"- docs/12 路径\n- out:sess_a:0123456789ABCDEF 大写\n- " + out + " 重复\n## 下一步\n- sess_a/13"
	got := parseRefs(body, "sess_a", []refCandidate{{ref: "other/7"}})
	assert.Eq(t, []string{out, "sess_a/12", "other/7"}, got)
	assert.Eq(t, 0, len(parseRefs("no section "+out, "sess_a", nil)))
}

// trace shows a segment break with its number, reason and token estimate.
func TestTraceSegmentLine(t *testing.T) {
	repo := openObserveRepo(t)
	prov := &scriptProv{name: "main", normal: func(n int, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
		return finalText(fmt.Sprintf("reply-%d", n))
	}}
	runner := compactRunner(t, repo, prov, nil, func(c *Config) { c.ContextWindow, c.CompactReserveTokens = 100000, 1000 })
	ctx := context.Background()
	res, err := runner.Run(ctx, RunRequest{Prompt: "hello " + strings.Repeat("h", 400)})
	assert.NoErr(t, err)
	out, err := runner.Compact(ctx, res.SessionID)
	assert.NoErr(t, err)
	assert.Eq(t, 2, out.Segment) // the start record is segment 1
	events, err := repo.ListEvents(ctx, store.EventFilter{SessionID: res.SessionID})
	assert.NoErr(t, err)
	var buf bytes.Buffer
	assert.NoErr(t, WriteTrace(&buf, events, nil, false))
	want := fmt.Sprintf("segment=#2 reason=manual tokens=%d->%d", out.TokensBefore, out.TokensAfter)
	assert.StrContains(t, buf.String(), want)
}
