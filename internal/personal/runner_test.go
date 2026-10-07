package personal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/data/testutil"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/storage/sqlite"
)

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
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	root := t.TempDir()
	putRule(t, filepath.Join(root, "AGENTS.md"), "first rule")
	provider := &rulesProvider{}
	// Keep one run store while recreating the runner to mimic --resume.
	runRoot := filepath.Join(t.TempDir(), "runs")
	newRunner := func(appendPrompt string) *Runner {
		runner, err := New(Config{Repository: repo, RunRoot: runRoot, Provider: provider,
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
	_, err = runner.Run(ctx, RunRequest{SessionID: model.SessionID, Prompt: "one"})
	assert.NoErr(t, err)
	putRule(t, filepath.Join(root, "AGENTS.md"), "second rule")
	runner = newRunner("ignored on resume")
	_, err = runner.Run(ctx, RunRequest{SessionID: model.SessionID, Prompt: "two"})
	assert.NoErr(t, err)
	_, err = runner.Run(ctx, RunRequest{SessionID: model.SessionID, Prompt: "three"})
	assert.NoErr(t, err)
	assert.Eq(t, 3, len(provider.requests))
	assert.Eq(t, true, strings.Contains(provider.requests[0].System, "first rule"))
	assert.Eq(t, true, strings.Contains(provider.requests[0].System, "extra"))
	assert.Eq(t, false, strings.Contains(provider.requests[1].System, "ignored on resume"))
	assert.Eq(t, true, strings.Contains(provider.requests[1].System, "second rule"))
	assert.Eq(t, 1, countRuleUpdates(provider.requests[1].Messages))
	assert.Eq(t, 0, countRuleUpdates(provider.requests[2].Messages))
	stored, err := repo.GetSession(ctx, model.SessionID)
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(stored.Metadata["personal_rules_content"].(string), "second rule"))
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
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	runRoot := filepath.Join(t.TempDir(), "runs")
	provider := &recallProvider{}

	firstRepo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	firstRunner, err := New(Config{Repository: firstRepo, RunRoot: runRoot, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	first, err := firstRunner.Run(ctx, RunRequest{Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, agentcore.RunCompleted, first.Status)
	assert.Eq(t, session.RunCompleted, first.State)
	if err := firstRepo.Close(); err != nil {
		t.Fatal(err)
	}

	secondRepo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondRepo.Close() })
	secondRunner, err := New(Config{Repository: secondRepo, RunRoot: runRoot, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	second, err := secondRunner.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "What was the code?"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, "amber", second.Output)
	assert.Eq(t, agentcore.RunCompleted, second.Status)
	assert.Eq(t, session.RunCompleted, second.State)
	assert.Eq(t, false, first.RunID == second.RunID)
	stored, err := secondRepo.GetSession(ctx, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, second.TurnID, stored.LastTurnID)
}

func TestRunnerUnknownResumeDoesNotCreateSession(t *testing.T) {
	ctx := context.Background()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, RunRoot: t.TempDir(), Provider: &recallProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(ctx, RunRequest{SessionID: "missing", Prompt: "continue"})
	assert.Eq(t, true, errors.Is(err, session.ErrSessionNotFound))
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

func TestRunnerCreatesGoferInjectedSessionID(t *testing.T) {
	ctx := context.Background()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	runner, err := New(Config{Repository: repo, RunRoot: t.TempDir(), Provider: &recallProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(ctx, RunRequest{SessionID: "gofer-001", CreateIfMissing: true, Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, "gofer-001", result.SessionID)
	assert.Eq(t, session.RunCompleted, result.State)
}

type priorTranscriptProvider struct{}

func (*priorTranscriptProvider) Name() string { return "prior-transcript-test" }

func (*priorTranscriptProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	var found bool
	for _, message := range req.Messages {
		found = found || message.Content == "Old host answer"
	}
	if !found {
		return nil, fmt.Errorf("existing Host transcript was not included")
	}
	return &agentcore.GenerateResult{Content: "legacy context found", FinishReason: agentcore.FinishFinal}, nil
}

func TestRunnerReadsExistingHostTranscript(t *testing.T) {
	ctx := context.Background()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	runRoot := filepath.Join(t.TempDir(), "runs")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	now := time.Now().UTC()
	if err := repo.CreateSession(ctx, session.Session{SessionID: "sess_legacy", Title: "legacy", Status: session.SessionOpen, LastTurnID: "turn_legacy", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateTurn(ctx, session.Turn{TurnID: "turn_legacy", SessionID: "sess_legacy", InitiatorType: session.InitiatorUser, Status: session.TurnCompleted, PrimaryRunID: "run_legacy", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRun(ctx, session.Run{RunID: "run_legacy", SessionID: "sess_legacy", TurnID: "turn_legacy", AgentID: "suag", State: session.RunCompleted, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendEvent(ctx, session.RunEvent{EventID: "evt_legacy", SessionID: "sess_legacy", TurnID: "turn_legacy", RunID: "run_legacy", AgentID: "suag", EventType: "run.completed", Source: "host.session", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	storage, err := sqlite.NewStorage(filepath.Join(runRoot, "sess_legacy", "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	oldMessages := []agentcore.Message{{Role: "user", Content: "Old host question"}, {Role: "assistant", Content: "Old host answer"}}
	if err := storage.SaveRun(ctx, &agentcore.RunState{RunID: "run_legacy", AgentID: "suag", Status: agentcore.RunCompleted, Messages: oldMessages}); err != nil {
		t.Fatal(err)
	}
	if err := storage.AppendMessages(ctx, "run_legacy", oldMessages...); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	runner, err := New(Config{Repository: repo, RunRoot: runRoot, Provider: &priorTranscriptProvider{}})
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

func TestRunnerResumesYieldedRunWithoutDuplicatingInput(t *testing.T) {
	ctx := context.Background()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	runRoot := filepath.Join(t.TempDir(), "runs")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	hook := &yieldOnceHook{}
	provider := &recallProvider{}
	runner, err := New(Config{Repository: repo, RunRoot: runRoot, Provider: provider, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runner.Run(ctx, RunRequest{Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, agentcore.RunYielded, first.Status)
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumed, err := New(Config{Repository: reopened, RunRoot: runRoot, Provider: provider, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resumed.Run(ctx, RunRequest{SessionID: first.SessionID, Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, first.RunID, second.RunID)
	assert.Eq(t, first.TurnID, second.TurnID)
	assert.Eq(t, agentcore.RunCompleted, second.Status)
	storage, err := sqlite.NewStorage(filepath.Join(runRoot, first.SessionID, "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	messages, err := storage.LoadMessages(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var originalCount int
	for _, message := range messages {
		if message.Content == "Remember code amber" {
			originalCount++
		}
	}
	assert.Eq(t, 1, originalCount)
}

func TestRunnerResumesYieldedRunWithoutNewInput(t *testing.T) {
	ctx := context.Background()
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	runRoot := filepath.Join(t.TempDir(), "runs")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	hook := &yieldOnceHook{}
	runner, err := New(Config{Repository: repo, RunRoot: runRoot, Provider: &recallProvider{}, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := runner.Run(ctx, RunRequest{Prompt: "Remember code amber"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	resumed, err := New(Config{Repository: reopened, RunRoot: runRoot, Provider: &recallProvider{}, Hooks: []agentcore.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resumed.Run(ctx, RunRequest{SessionID: first.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, first.RunID, second.RunID)
	assert.Eq(t, session.RunCompleted, second.State)
	storage, err := sqlite.NewStorage(filepath.Join(runRoot, first.SessionID, "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	messages, err := storage.LoadMessages(ctx, first.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var users int
	for _, message := range messages {
		if message.Role == "user" {
			users++
		}
	}
	assert.Eq(t, 1, users)
}

type blockingProvider struct{ started chan struct{} }

func (*blockingProvider) Name() string { return "blocking-test" }

func (p *blockingProvider) Generate(ctx context.Context, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRunnerPersistsCancellationAfterContextEnds(t *testing.T) {
	dbPath := testutil.NewMigratedSQLitePath(t, "sessions.db")
	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	provider := &blockingProvider{started: make(chan struct{})}
	runner, err := New(Config{Repository: repo, RunRoot: t.TempDir(), Provider: provider})
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
	assert.Eq(t, session.RunCanceled, result.State)
	storedRun, err := repo.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, session.RunCanceled, storedRun.State)
	storedSession, err := repo.GetSession(context.Background(), result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	assert.Eq(t, session.SessionOpen, storedSession.Status)
}
