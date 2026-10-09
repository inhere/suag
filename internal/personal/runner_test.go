package personal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

func TestRunnerWritesResumedTurnsAndToolSummariesToTranscript(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	root := t.TempDir()
	registry := tools.NewRegistry()
	registry.Register(transcriptTool{})
	runner, err := New(Config{
		Repository: repo,
		Provider: mock.NewProvider(
			mock.ToolCall("call-1", "shell_exec", map[string]any{"command": "echo safe"}),
			mock.Final("first answer"),
			mock.Final("resumed answer"),
		),
		Tools: registry, TranscriptDir: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runner.Run(ctx, RunRequest{Prompt: "run tool"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "resume"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, first.SessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	if len(entries) != 5 {
		t.Fatalf("expected user/tool/assistant for both turns, got %#v", entries)
	}
	if entries[1]["type"] != "tool" || entries[1]["name"] != "shell_exec" || strings.Contains(entries[1]["summary"].(string), "canary-credential-4a2b") {
		t.Fatalf("tool summary was not redacted: %#v", entries[1])
	}
	if entries[4]["type"] != "assistant" || entries[4]["text"] != "resumed answer" {
		t.Fatalf("resumed turn was not appended: %#v", entries[4])
	}
}

type transcriptTool struct{}

func (transcriptTool) Name() string        { return "shell_exec" }
func (transcriptTool) Description() string { return "test shell tool" }
func (transcriptTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: "shell_exec"}
}
func (transcriptTool) Kind() tools.Kind { return tools.KindNative }
func (transcriptTool) Execute(context.Context, agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return &agentcore.ToolResult{Name: "shell_exec", Content: `Authorization: Bearer canary-credential-4a2b {"api_key":"canary-credential-4a2b"}`}, nil
}

type recallProvider struct{ requests [][]agentcore.Message }

type rulesProvider struct{ requests []*agentcore.GenerateRequest }

func (*rulesProvider) Name() string { return "rules-test" }

func (p *rulesProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	copyReq := *req
	copyReq.Messages = append([]agentcore.Message(nil), req.Messages...)
	p.requests = append(p.requests, &copyReq)
	return &agentcore.GenerateResult{Content: "ok", FinishReason: agentcore.FinishFinal}, nil
}

func TestRunnerRulesPersistAndUpdateOnlyWhenChanged(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	root := t.TempDir()
	putRule(t, filepath.Join(root, "AGENTS.md"), "first rule")
	provider := &rulesProvider{}
	// Keep one run store while recreating the runner to mimic --resume.
	newRunner := func(appendPrompt string) *Runner {
		runner, err := New(Config{Repository: repo, Provider: provider,
			RuleSource:         func() (string, string, error) { return LoadRules(root, "", false) },
			AppendSystemPrompt: appendPrompt})
		if err != nil {
			t.Fatal(err)
		}
		return runner
	}
	runner := newRunner("extra")
	model, err := runner.NewSession(ctx, "rules-session", "test")
	assert.NoErr(t, err)
	_, err = runner.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "one"})
	assert.NoErr(t, err)
	putRule(t, filepath.Join(root, "AGENTS.md"), "second rule")
	runner = newRunner("replaced on resume")
	_, err = runner.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "two"})
	assert.NoErr(t, err)
	_, err = runner.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "three"})
	assert.NoErr(t, err)
	assert.Eq(t, 3, len(provider.requests))
	assert.Eq(t, true, strings.Contains(provider.requests[0].System, "first rule"))
	assert.Eq(t, true, strings.Contains(provider.requests[0].System, "extra"))
	assert.Eq(t, true, strings.Contains(provider.requests[1].System, "replaced on resume"))
	assert.Eq(t, false, strings.Contains(provider.requests[1].System, "extra"))
	assert.Eq(t, 1, countNotices(provider.requests[1].Messages, "附加提示已更新"))
	assert.Eq(t, 0, countNotices(provider.requests[2].Messages, "附加提示已更新"))
	assert.Eq(t, true, strings.Contains(provider.requests[1].System, "second rule"))
	assert.Eq(t, 1, countRuleUpdates(provider.requests[1].Messages))
	// Full rule text appears once (system prompt only), not again in the update message.
	for _, m := range provider.requests[1].Messages {
		assert.Eq(t, false, strings.Contains(m.Content, "second rule"))
	}
	assert.Eq(t, 1, strings.Count(provider.requests[1].System, "second rule"))
	assert.Eq(t, 0, countRuleUpdates(provider.requests[2].Messages))
	stored, err := repo.GetSession(ctx, model.ID)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(stored.Meta["personal_rules_content"].(string), "second rule"))
}

func countRuleUpdates(messages []agentcore.Message) int {
	n := 0
	for _, message := range messages {
		if message.Role == "system" && strings.HasPrefix(message.Content, "项目规则已更新") {
			n++
		}
	}
	return n
}

func (*recallProvider) Name() string { return "recall-test" }

func (p *recallProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.requests = append(p.requests, append([]agentcore.Message(nil), req.Messages...))
	if len(p.requests) == 1 {
		return &agentcore.GenerateResult{Content: "The code is amber", FinishReason: agentcore.FinishFinal}, nil
	}
	var priorUser, priorAnswer bool
	for _, message := range req.Messages {
		priorUser = priorUser || message.Content == "Remember code amber"
		priorAnswer = priorAnswer || message.Content == "The code is amber"
	}
	if !priorUser || !priorAnswer {
		return nil, fmt.Errorf("previous authoritative transcript was not supplied to provider")
	}
	return &agentcore.GenerateResult{Content: "amber", FinishReason: agentcore.FinishFinal}, nil
}

func TestRunnerRestoresCompletedConversationAfterReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	provider := &recallProvider{}

	firstRepo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	firstRunner, err := New(Config{Repository: firstRepo, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstRunner.Run(ctx, RunRequest{Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, agentcore.RunCompleted, first.Status)
	assert.Eq(t, RunCompleted, first.State)
	if err := firstRepo.Close(); err != nil {
		t.Fatal(err)
	}

	secondRepo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondRepo.Close() })
	secondRunner, err := New(Config{Repository: secondRepo, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondRunner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "What was the code?"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, "amber", second.Output)
	assert.Eq(t, agentcore.RunCompleted, second.Status)
	assert.Eq(t, RunCompleted, second.State)
	assert.Eq(t, false, first.RunID == second.RunID)
	stored, err := secondRepo.GetSession(ctx, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, second.TurnID, stored.LastTurnID)
}

func TestRunnerUnknownResumeDoesNotCreateSession(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, Provider: &recallProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(ctx, RunRequest{SessionID: "missing", Prompt: "continue"})
	assert.Eq(t, true, errors.Is(err, ErrSessionNotFound))
	sessions, err := repo.ListSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, 0, len(sessions))
	_, err = runner.Run(ctx, RunRequest{SessionID: "../escape", CreateIfMissing: true, Prompt: "bad"})
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "invalid session ID"))
	_, err = runner.Run(ctx, RunRequest{CreateIfMissing: true})
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "prompt is required"))
}

func TestRunnerCreatesCallerSuppliedSessionID(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, Provider: &recallProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(ctx, RunRequest{SessionID: "gofer-001", CreateIfMissing: true, Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, "gofer-001", result.SessionID)
	assert.Eq(t, RunCompleted, result.State)
}

type priorTranscriptProvider struct{}

func (*priorTranscriptProvider) Name() string { return "prior-transcript-test" }

func (*priorTranscriptProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	var found bool
	for _, message := range req.Messages {
		found = found || message.Content == "Old host answer"
	}
	if !found {
		return nil, fmt.Errorf("existing store history was not included")
	}
	return &agentcore.GenerateResult{Content: "legacy context found", FinishReason: agentcore.FinishFinal}, nil
}

// History is rebuilt from the completed turns' entries written straight into the store.
func TestRunnerReadsExistingStoreHistory(t *testing.T) {
	ctx := context.Background()
	repo, err := store.Open(newStorePath(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	assert.NoErr(t, repo.CreateSession(ctx, store.Session{ID: "sess_legacy", Title: "legacy"}))
	assert.NoErr(t, repo.CreateTurn(ctx, store.Turn{ID: "turn_legacy", SessionID: "sess_legacy", Status: store.TurnCompleted}))
	assert.NoErr(t, repo.AgentStorage().AppendMessages(ctx, "turn_legacy",
		agentcore.Message{Role: "user", Content: "Old host question"}, agentcore.Message{Role: "assistant", Content: "Old host answer"}))

	runner, err := New(Config{Repository: repo, Provider: &priorTranscriptProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(ctx, RunRequest{SessionID: "sess_legacy", Prompt: "Use the previous answer"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, "legacy context found", result.Output)
}

type yieldOnceHook struct {
	agentcore.NopHook
	yielded bool
}

func (h *yieldOnceHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	if !h.yielded {
		h.yielded = true
		ctrl.Yield("input_required", nil)
	}
	return nil
}

// A yielded turn is reported as INPUT_REQUIRED, is not resumed in place, and
// its messages are history for the next (new) turn.
func TestRunnerYieldedTurnIsNotResumedAndBecomesHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	hook := &yieldOnceHook{}
	runner, err := New(Config{Repository: repo, Provider: &recallProvider{}, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runner.Run(ctx, RunRequest{Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, agentcore.RunYielded, first.Status)
	assert.Eq(t, RunInputRequired, first.State)
	assert.NoErr(t, repo.Close())

	reopened, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumed, err := New(Config{Repository: reopened, Provider: &recallProvider{}, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resumed.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "What was the code?"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, false, first.RunID == second.RunID)
	assert.Eq(t, RunCompleted, second.State)
	assert.Eq(t, true, strings.Contains(second.Output, "amber"))
	// An empty prompt no longer resumes a waiting run.
	_, err = resumed.Run(ctx, RunRequest{SessionID: first.SessionID})
	assert.Eq(t, true, err != nil && strings.Contains(err.Error(), "prompt is required"))
	// run_id == turn_id: both turns are stored and completed.
	turns, err := reopened.ListTurns(ctx, first.SessionID, store.TurnCompleted)
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(turns))
}

type blockingProvider struct{ started chan struct{} }

func (*blockingProvider) Name() string { return "blocking-test" }

func (p *blockingProvider) Generate(ctx context.Context, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunnerPersistsCancellationAfterContextEnds(t *testing.T) {
	dbPath := newStorePath(t)
	repo, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	provider := &blockingProvider{started: make(chan struct{})}
	runner, err := New(Config{Repository: repo, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resultCh := make(chan RunResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, runErr := runner.Run(ctx, RunRequest{Prompt: "wait"})
		resultCh <- result
		errCh <- runErr
	}()
	select {
	case <-provider.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	result := <-resultCh
	assert.Eq(t, true, errors.Is(<-errCh, context.Canceled))
	assert.Eq(t, RunCanceled, result.State)
	storedTurn, err := repo.GetTurn(context.Background(), result.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, store.TurnCancelled, storedTurn.Status)
	assert.Eq(t, result.TurnID, result.RunID)
	storedSession, err := repo.GetSession(context.Background(), result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, store.SessionOpen, storedSession.Status)
}

func countNotices(messages []agentcore.Message, prefix string) int {
	n := 0
	for _, message := range messages {
		if message.Role == "system" && strings.Contains(message.Content, prefix) {
			n++
		}
	}
	return n
}

func TestRunnerAppendPromptOnResume(t *testing.T) {
	ctx := context.Background()
	const notice = "附加提示已更新"
	// Each case resumes a session created with "orig" using a different runner append prompt.
	cases := []struct {
		name, resume, wantStored string
		wantNotice               int
	}{
		{"different value updates and notifies once", "fresh-skill-text", "fresh-skill-text", 1},
		{"empty keeps persisted value", "", "orig", 0},
		{"same value does nothing", "orig", "orig", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := store.Open(newStorePath(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repo.Close() })
			provider := &rulesProvider{}
			newRunner := func(appendPrompt string) *Runner {
				runner, err := New(Config{Repository: repo, Provider: provider, AppendSystemPrompt: appendPrompt})
				if err != nil {
					t.Fatal(err)
				}
				return runner
			}
			model, err := newRunner("orig").NewSession(ctx, "append-session", "test")
			assert.NoErr(t, err)
			resumed := newRunner(tc.resume)
			_, err = resumed.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "one"})
			assert.NoErr(t, err)
			_, err = resumed.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "two"})
			assert.NoErr(t, err)
			assert.Eq(t, 2, len(provider.requests))
			stored, err := repo.GetSession(ctx, model.ID)
			assert.NoErr(t, err)
			assert.Eq(t, tc.wantStored, stored.Meta["personal_append_prompt"])
			assert.Eq(t, true, strings.Contains(provider.requests[0].System, tc.wantStored))
			assert.Eq(t, tc.wantNotice, countNotices(provider.requests[0].Messages, notice))
			assert.Eq(t, 0, countNotices(provider.requests[1].Messages, notice))
			for _, m := range provider.requests[0].Messages {
				assert.Eq(t, false, strings.Contains(m.Content, "fresh-skill-text"))
			}
		})
	}
}

// newStorePath returns a fresh core store path under a temp dir.
func newStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "db", "sessions.db")
}
