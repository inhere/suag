package intrunner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/api"
)

func TestRunner_RunReusesSessionMemoryAcrossTurns(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").
		QueueText("first answer").
		QueueText("second answer")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "second task",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessage(calls[1].Messages, "user", "first task"))
	assert.True(t, containsMessage(calls[1].Messages, "assistant", "first answer"))
}

func TestRunner_RunEmitsPreparingAndThinkingStates(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var states []string
	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_001",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "do something",
		Emit: func(event Event) error {
			if event.EventType != "run.status" {
				return nil
			}
			state, _ := event.Payload["state"].(string)
			states = append(states, state)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, 2, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
}

func TestRunner_RunEmitsModelAndToolProgressActivities(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("done")
	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})

	runner, err := New(Config{
		Provider: mock,
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var activities []map[string]any
	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_progress",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "use echo",
		Emit: func(event Event) error {
			if event.EventType != session.EventRunExternalActivity {
				return nil
			}
			activities = append(activities, event.Payload)
			return nil
		},
	})
	assert.NoErr(t, err)

	assert.Eq(t, 6, len(activities))
	assert.Eq(t, "model", activities[0]["kind"])
	assert.Eq(t, "LLM request", activities[0]["title"])
	assert.Eq(t, "running", activities[0]["status"])
	assert.Eq(t, "LLM returned tool calls", activities[1]["title"])
	assert.Eq(t, "tool", activities[2]["kind"])
	assert.Eq(t, "Tool echo", activities[2]["title"])
	assert.Eq(t, "running", activities[2]["status"])
	assert.Eq(t, "completed", activities[3]["status"])
	assert.Eq(t, "LLM request", activities[4]["title"])
	assert.Eq(t, "LLM completed", activities[5]["title"])
}

func TestRunner_RunUsesResolvedToolSubset(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})
	registry.MustRegister(&hostAnotherTool{})

	mock := llm.NewMockProvider("mock").QueueText("done")
	runner, err := New(Config{
		Provider: mock,
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_subset",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "do something",
		Context: map[string]any{
			runtimeContextResolvedToolNamesKey: []string{"echo"},
		},
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, toolSchemaListed(calls[0].Tools, "echo"))
	assert.False(t, toolSchemaListed(calls[0].Tools, "another"))
}

func TestRunner_RunUsesReadOnlyResolvedToolSubset(t *testing.T) {
	t.Parallel()

	registry := tools.NewRegistry()
	registry.MustRegister(&hostNamedTool{name: "file_read"})
	registry.MustRegister(&hostNamedTool{name: "file_write"})
	registry.MustRegister(&hostNamedTool{name: "shell_exec"})
	registry.MustRegister(&hostNamedTool{name: "file_list"})

	mock := llm.NewMockProvider("mock").QueueText("done")
	runner, err := New(Config{
		Provider: mock,
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_read_only_subset",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "inspect only",
		Context: map[string]any{
			runtimeContextResolvedToolNamesKey: []string{"file_read", "file_list"},
		},
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, toolSchemaListed(calls[0].Tools, "file_read"))
	assert.True(t, toolSchemaListed(calls[0].Tools, "file_list"))
	assert.False(t, toolSchemaListed(calls[0].Tools, "file_write"))
	assert.False(t, toolSchemaListed(calls[0].Tools, "shell_exec"))
}

func TestRunner_RunInjectsResolvedSystemFragmentsIntoProviderContext(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")
	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_skill_fragment",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "brainstorm product direction",
		Context: map[string]any{
			runtimeContextResolvedSystemFragmentsKey: []string{
				"[Skill office-hours] Ask questions before writing code.",
			},
		},
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Skill office-hours] Ask questions before writing code."))
}

func TestRunner_RunInjectsResolvedResourceHintsIntoProviderContext(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	resourceDir := filepath.Join(workspace, "skills", "office-hours", "references")
	err := os.MkdirAll(resourceDir, 0o755)
	assert.NoErr(t, err)
	resourcePath := filepath.Join(resourceDir, "guide.md")
	err = os.WriteFile(resourcePath, []byte("# Guide\n\nAlways ask one clarifying question before implementation.\n"), 0o644)
	assert.NoErr(t, err)
	skillPath := filepath.Join(workspace, "skills", "office-hours", "SKILL.md")
	err = os.WriteFile(skillPath, []byte("---\nname: office-hours\ndescription: Ask questions.\n"), 0o644)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")
	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_skill_resource",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "brainstorm product direction",
		Context: map[string]any{
			"resolved_resource_hints": []map[string]any{
				{
					"skill":       "office-hours",
					"kind":        "reference",
					"path":        resourcePath,
					"source_path": skillPath,
					"priority":    120,
				},
			},
		},
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Skill Resource office-hours/reference guide.md]"))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "Always ask one clarifying question before implementation."))
}

func TestRunner_RunSkipsResolvedResourceHintsOutsideSkillRoot(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	resourceDir := filepath.Join(workspace, "skills", "office-hours", "references")
	err := os.MkdirAll(resourceDir, 0o755)
	assert.NoErr(t, err)
	skillPath := filepath.Join(workspace, "skills", "office-hours", "SKILL.md")
	err = os.WriteFile(skillPath, []byte("---\nname: office-hours\ndescription: Ask questions.\n"), 0o644)
	assert.NoErr(t, err)
	outsidePath := filepath.Join(t.TempDir(), "guide.md")
	err = os.WriteFile(outsidePath, []byte("outside skill root\n"), 0o644)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")
	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_skill_resource_outside",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "brainstorm product direction",
		Context: map[string]any{
			"resolved_resource_hints": []map[string]any{
				{
					"skill":       "office-hours",
					"kind":        "reference",
					"path":        outsidePath,
					"source_path": skillPath,
					"priority":    120,
				},
			},
		},
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "[Skill Resource office-hours/reference"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "outside skill root"))
}

func TestRunner_RunInjectsFormalMemoryIntoProviderContext(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(memoryRoot, "memory", "MEMORY.md"), []byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] prefers concise execution notes\n"), 0o644)
	assert.NoErr(t, err)
	err = os.WriteFile(filepath.Join(memoryRoot, "memory", "USER.md"), []byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] 优先中文回复\n"), 0o644)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_memory",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Host MEMORY]\n- [turn_learning] prefers concise execution notes"))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "[Host USER]\n- [user_preference] 优先中文回复"))
}

func TestRunner_RunSkipsMissingFormalMemoryFiles(t *testing.T) {
	t.Parallel()

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_no_memory",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "[Host MEMORY]"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "[Host USER]"))
}

func TestRunner_RunRefreshesFormalMemoryAcrossTurns(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)

	writeFormal := func(content string) {
		err := os.WriteFile(filepath.Join(memoryRoot, "memory", "MEMORY.md"), []byte(content), 0o644)
		assert.NoErr(t, err)
	}
	writeFormal("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] first memory\n")

	mock := llm.NewMockProvider("mock").
		QueueText("first answer").
		QueueText("second answer")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_refresh",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)

	writeFormal("# MEMORY\n\nUpdated: 2026-04-20T00:01:00Z\n\n- [turn_learning] second memory\n")

	_, err = runner.Run(context.Background(), Request{
		SessionID:   "sess_refresh",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "second task",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "first memory"))
	assert.True(t, containsMessageContains(calls[1].Messages, "system", "second memory"))
	assert.False(t, containsMessageContains(calls[1].Messages, "system", "first memory"))
}

func TestRunner_RunReturnsFormalMemoryRecallMetadata(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] prefer concise notes\n- [turn_learning] capture next step\n"),
		0o644,
	)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "USER.md"),
		[]byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] 优先中文回复\n"),
		0o644,
	)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_recall_meta",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	recall, ok := result.Metadata["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall] type = %T, want map[string]any", result.Metadata["memory_recall"])
	}
	assert.Eq(t, true, recall["enabled"])
	assert.Eq(t, "query_aware_lite", recall["strategy"])
	assert.Eq(t, 2, recall["entry_count"])
	assert.Eq(t, 0, recall["matched_entry_count"])
	assert.Eq(t, true, recall["fallback_used"])
	sources, ok := recall["sources"].([]map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall][sources] type = %T, want []map[string]any", recall["sources"])
	}
	assert.Eq(t, 2, len(sources))
	assert.Eq(t, "MEMORY", sources[0]["name"])
	assert.Eq(t, 1, sources[0]["entry_count"])
	assert.Eq(t, "USER", sources[1]["name"])
	assert.Eq(t, 1, sources[1]["entry_count"])
}

func TestRunner_RunUsesQueryAwareRecallWhenInstructionMatches(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] database migration should be verified before release\n- [turn_learning] ui spacing should stay compact\n"),
		0o644,
	)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "USER.md"),
		[]byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] prefers concise updates\n"),
		0o644,
	)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_query_aware_match",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "please verify the database migration flow",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "database migration should be verified before release"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "ui spacing should stay compact"))

	recall, ok := result.Metadata["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall] type = %T, want map[string]any", result.Metadata["memory_recall"])
	}
	assert.Eq(t, "query_aware_lite", recall["strategy"])
	assert.Eq(t, 1, recall["matched_entry_count"])
	assert.Eq(t, false, recall["fallback_used"])
	assert.Eq(t, 1, recall["entry_count"])
}

func TestRunner_RunFallsBackWhenInstructionHasNoMemoryMatch(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] database migration should be verified before release\n- [turn_learning] ui spacing should stay compact\n- [turn_learning] cron cleanup should stay idempotent\n"),
		0o644,
	)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "USER.md"),
		[]byte("# USER\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [user_preference] prefers concise updates\n- [user_preference] prefers Chinese answers\n"),
		0o644,
	)
	assert.NoErr(t, err)

	mock := llm.NewMockProvider("mock").QueueText("done")

	runner, err := New(Config{
		Provider:   mock,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_query_aware_fallback",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "please handle websocket origin issue",
	})
	assert.NoErr(t, err)

	calls := mock.Calls()
	assert.Eq(t, 1, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "database migration should be verified before release"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "cron cleanup should stay idempotent"))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "prefers concise updates"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "prefers Chinese answers"))

	recall, ok := result.Metadata["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("Run().Metadata[memory_recall] type = %T, want map[string]any", result.Metadata["memory_recall"])
	}
	assert.Eq(t, "query_aware_lite", recall["strategy"])
	assert.Eq(t, 0, recall["matched_entry_count"])
	assert.Eq(t, true, recall["fallback_used"])
	assert.Eq(t, 2, recall["entry_count"])
}

func TestLoadFormalMemoryRecallMetadataReturnsClonedMetadata(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] 先查看相关文件并分析问题，不要直接修改\n"),
		0o644,
	)
	assert.NoErr(t, err)

	metadata := LoadFormalMemoryRecallMetadata(memoryRoot, "继续处理一下")
	recall, ok := metadata["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata()[memory_recall] type = %T, want map[string]any", metadata["memory_recall"])
	}
	sources, ok := recall["sources"].([]map[string]any)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata()[memory_recall][sources] type = %T, want []map[string]any", recall["sources"])
	}
	assert.Eq(t, 1, len(sources))
	preview, ok := sources[0]["preview"].([]string)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata()[memory_recall][sources][0][preview] type = %T, want []string", sources[0]["preview"])
	}
	assert.Eq(t, 1, len(preview))

	preview[0] = "changed"
	metadataAgain := LoadFormalMemoryRecallMetadata(memoryRoot, "继续处理一下")
	recallAgain, ok := metadataAgain["memory_recall"].(map[string]any)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata() second call [memory_recall] type = %T, want map[string]any", metadataAgain["memory_recall"])
	}
	sourcesAgain, ok := recallAgain["sources"].([]map[string]any)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata() second call [memory_recall][sources] type = %T, want []map[string]any", recallAgain["sources"])
	}
	previewAgain, ok := sourcesAgain[0]["preview"].([]string)
	if !ok {
		t.Fatalf("LoadFormalMemoryRecallMetadata() second call preview type = %T, want []string", sourcesAgain[0]["preview"])
	}
	assert.True(t, containsText(previewAgain[0], "先查看相关文件并分析问题"))
}

func TestRunner_RunRecallCanAffectFinalSummary(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] always mention migration checklist in final summary\n"),
		0o644,
	)
	assert.NoErr(t, err)

	provider := &summaryFromContextProvider{}
	runner, err := New(Config{
		Provider:   provider,
		Registry:   tools.NewRegistry(),
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_recall_effect",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "please review the migration rollout",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "final summary uses recalled memory", result.Summary)
}

func TestRunner_RunReappliesRecallForResumedTurn(t *testing.T) {
	t.Parallel()

	memoryRoot := t.TempDir()
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-20T00:00:00Z\n\n- [turn_learning] alphaonly migrationcheck\n- [turn_learning] betaonly websocketorigin\n"),
		0o644,
	)
	assert.NoErr(t, err)

	hook := &yieldBeforeSecondModelHostHook{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "persisted"}).
		QueueText("done after continue")

	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})

	runner, err := New(Config{
		Provider:   mock,
		Registry:   registry,
		Tracer:     tracing.NewInMemoryTracer(nil),
		RunRoot:    t.TempDir(),
		MemoryRoot: memoryRoot,
		Hooks:      []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	first, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume_recall",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "please verify alphaonly",
	})
	assert.NoErr(t, err)
	assert.Eq(t, session.RunInputRequired, first.WaitState)

	second, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume_recall",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "please continue betaonly",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "done after continue", second.Summary)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessageContains(calls[0].Messages, "system", "alphaonly migrationcheck"))
	assert.False(t, containsMessageContains(calls[0].Messages, "system", "betaonly websocketorigin"))
	assert.True(t, containsMessageContains(calls[1].Messages, "system", "betaonly websocketorigin"))
	assert.False(t, containsMessageContains(calls[1].Messages, "system", "alphaonly migrationcheck"))
	assert.True(t, containsMessage(calls[1].Messages, "tool", "echo: persisted"))
	assert.True(t, containsMessage(calls[1].Messages, "user", "please continue betaonly"))
}

func TestRunner_RunReusesSuspendedRunnerOnContinue(t *testing.T) {
	t.Parallel()

	hook := &yieldBeforeSecondModelHostHook{}
	mock := llm.NewMockProvider("mock").
		QueueToolCall("echo", map[string]any{"msg": "persisted"}).
		QueueText("done after continue")

	registry := tools.NewRegistry()
	registry.MustRegister(&hostEchoTool{})

	runner, err := New(Config{
		Provider: mock,
		Registry: registry,
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	first, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "first task",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_resume", first.Summary)
	assert.Eq(t, session.RunInputRequired, first.WaitState)

	second, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_resume",
		TurnID:      "turn_002",
		RunID:       "run_002",
		AgentID:     "default",
		Instruction: "continue with human input",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "", string(second.WaitState))
	assert.Eq(t, "done after continue", second.Summary)

	calls := mock.Calls()
	assert.Eq(t, 2, len(calls))
	assert.True(t, containsMessage(calls[1].Messages, "tool", "echo: persisted"))
	assert.True(t, containsMessage(calls[1].Messages, "user", "continue with human input"))
}

func TestRunner_RunAddsInitialLoopMetadataToYieldedWait(t *testing.T) {
	t.Parallel()

	hook := &yieldOnFirstModelHostHook{}
	mock := llm.NewMockProvider("mock")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var statusPayloads []map[string]any
	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_initial_wait",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "pause immediately",
		Emit: func(event Event) error {
			if event.EventType != session.EventRunStatus {
				return nil
			}
			payload := map[string]any{}
			for key, value := range event.Payload {
				payload[key] = value
			}
			statusPayloads = append(statusPayloads, payload)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, session.RunInputRequired, result.WaitState)
	assert.Eq(t, "initial", result.WaitMetadata["loop_phase"])
	assert.Eq(t, 0, result.WaitMetadata["resume_count"])
	if payload, ok := statusPayloads[2]["waitMetadata"].(map[string]any); ok {
		assert.Eq(t, "initial", payload["loop_phase"])
		assert.Eq(t, 0, payload["resume_count"])
	} else {
		t.Fatalf("status payload waitMetadata type = %T, want map[string]any", statusPayloads[2]["waitMetadata"])
	}
}

func TestRunner_RunAddsResumedLoopMetadataForResumeModes(t *testing.T) {
	t.Parallel()

	t.Run("resume with input", func(t *testing.T) {
		hook := &yieldOnFirstTwoModelsHostHook{}
		mock := llm.NewMockProvider("mock")

		runner, err := New(Config{
			Provider: mock,
			Registry: tools.NewRegistry(),
			Tracer:   tracing.NewInMemoryTracer(nil),
			RunRoot:  t.TempDir(),
			Hooks:    []agentcore.Hook{hook},
		})
		assert.NoErr(t, err)
		t.Cleanup(func() {
			assert.NoErr(t, runner.Close())
		})

		_, err = runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_input",
			TurnID:      "turn_001",
			RunID:       "run_001",
			AgentID:     "default",
			Instruction: "first wait",
		})
		assert.NoErr(t, err)

		result, err := runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_input",
			TurnID:      "turn_002",
			RunID:       "run_002",
			AgentID:     "default",
			Instruction: "resume with message",
		})
		assert.NoErr(t, err)
		assert.Eq(t, session.RunInputRequired, result.WaitState)
		assert.Eq(t, "resumed", result.WaitMetadata["loop_phase"])
		assert.Eq(t, "resume_with_input", result.WaitMetadata["resume_source"])
		assert.Eq(t, 1, result.WaitMetadata["resume_count"])
	})

	t.Run("resume from state", func(t *testing.T) {
		hook := &yieldOnFirstTwoModelsHostHook{}
		mock := llm.NewMockProvider("mock")

		runner, err := New(Config{
			Provider: mock,
			Registry: tools.NewRegistry(),
			Tracer:   tracing.NewInMemoryTracer(nil),
			RunRoot:  t.TempDir(),
			Hooks:    []agentcore.Hook{hook},
		})
		assert.NoErr(t, err)
		t.Cleanup(func() {
			assert.NoErr(t, runner.Close())
		})

		_, err = runner.Run(context.Background(), Request{
			SessionID:   "sess_resume_wait_state",
			TurnID:      "turn_001",
			RunID:       "run_001",
			AgentID:     "default",
			Instruction: "first wait",
		})
		assert.NoErr(t, err)

		result, err := runner.Run(context.Background(), Request{
			SessionID: "sess_resume_wait_state",
			TurnID:    "turn_002",
			RunID:     "run_002",
			AgentID:   "default",
		})
		assert.NoErr(t, err)
		assert.Eq(t, session.RunInputRequired, result.WaitState)
		assert.Eq(t, "resumed", result.WaitMetadata["loop_phase"])
		assert.Eq(t, "resume_from_state", result.WaitMetadata["resume_source"])
		assert.Eq(t, 1, result.WaitMetadata["resume_count"])
	})
}

func TestRunner_RunMapsApprovalYieldToAuthRequired(t *testing.T) {
	t.Parallel()

	hook := &yieldApprovalHostHook{}
	mock := llm.NewMockProvider("mock")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	var states []string
	var statusPayloads []map[string]any
	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_auth",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "request approval",
		Emit: func(event Event) error {
			if event.EventType != session.EventRunStatus {
				return nil
			}
			state, _ := event.Payload["state"].(string)
			states = append(states, state)
			payload := map[string]any{}
			for key, value := range event.Payload {
				payload[key] = value
			}
			statusPayloads = append(statusPayloads, payload)
			return nil
		},
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_approval", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
	assert.Eq(t, "manual", result.WaitMetadata["policy"])
	if payload, ok := statusPayloads[2]["waitMetadata"].(map[string]any); ok {
		assert.Eq(t, "manual", payload["policy"])
	} else {
		t.Fatalf("states[2].Payload[waitMetadata] type = %T, want map[string]any", statusPayloads[2]["waitMetadata"])
	}
	assert.Eq(t, 3, len(states))
	assert.Eq(t, "PREPARING", states[0])
	assert.Eq(t, "THINKING", states[1])
	assert.Eq(t, "AUTH_REQUIRED", states[2])
}

func TestRunner_RunUsesStructuredWaitKindBeforeSummaryFallback(t *testing.T) {
	t.Parallel()

	hook := &yieldStructuredInputHostHook{}
	mock := llm.NewMockProvider("mock")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_wait_kind",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "needs more info",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_review", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
}

func TestRunner_RunKeepsSummaryFallbackWhenWaitKindMissing(t *testing.T) {
	t.Parallel()

	hook := &yieldApprovalHostHook{}
	mock := llm.NewMockProvider("mock")

	runner, err := New(Config{
		Provider: mock,
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Hooks:    []agentcore.Hook{hook},
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	result, err := runner.Run(context.Background(), Request{
		SessionID:   "sess_wait_fallback",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "request approval",
	})
	assert.NoErr(t, err)
	assert.Eq(t, "await_approval", result.Summary)
	assert.Eq(t, session.RunAuthRequired, result.WaitState)
}

func TestRunner_ValidateContinueRejectsWhenSuspendedRuntimeIsMissing(t *testing.T) {
	t.Parallel()

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock"),
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	err = runner.ValidateContinue(context.Background(), Request{
		SessionID: "sess_resume_missing",
		Context: map[string]any{
			"resume_turn_id":     "turn_001",
			"resume_from_run_id": "run_001",
		},
	})
	assert.Err(t, err)
	assert.Eq(t, "waiting runtime is no longer available; start a new turn or reissue the task", err.Error())
}

func TestRunner_RuntimeKindDefaultsToAgentcore(t *testing.T) {
	t.Parallel()

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock"),
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	assert.Eq(t, agent.RuntimeAgentcore, runner.runtimeKind())
}

func TestRunner_RuntimeKindAllowsExplicitReactFallback(t *testing.T) {
	t.Parallel()

	runner, err := New(Config{
		Provider: llm.NewMockProvider("mock"),
		Registry: tools.NewRegistry(),
		Tracer:   tracing.NewInMemoryTracer(nil),
		RunRoot:  t.TempDir(),
		Runtime:  " react ",
	})
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, runner.Close())
	})

	assert.Eq(t, agent.RuntimeReact, runner.runtimeKind())
}

type yieldBeforeSecondModelHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldBeforeSecondModelHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"step": "after_tool"})
	}
	return nil
}

type yieldApprovalHostHook struct {
	agentcore.NopHook
}

func (h *yieldApprovalHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("await_approval", map[string]any{"policy": "manual"})
	return nil
}

type yieldStructuredInputHostHook struct {
	agentcore.NopHook
}

type yieldOnFirstModelHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldOnFirstModelHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 1 {
		ctrl.Yield("await_resume", map[string]any{"wait_kind": "input_required"})
	}
	return nil
}

type yieldOnFirstTwoModelsHostHook struct {
	agentcore.NopHook
	beforeModelCalls int
}

func (h *yieldOnFirstTwoModelsHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	h.beforeModelCalls++
	if h.beforeModelCalls == 1 || h.beforeModelCalls == 2 {
		ctrl.Yield("await_resume", map[string]any{"wait_kind": "input_required"})
	}
	return nil
}

func (h *yieldStructuredInputHostHook) BeforeModel(_ context.Context, _ *agentcore.ModelEvent, ctrl *agentcore.Control) error {
	ctrl.Yield("await_review", map[string]any{
		"wait_kind": "auth_required",
		"policy":    "manual_review",
	})
	return nil
}

type hostEchoTool struct{}

func (e *hostEchoTool) Name() string        { return "echo" }
func (e *hostEchoTool) Description() string { return "echo the msg argument" }
func (e *hostEchoTool) Kind() tools.Kind    { return tools.KindNative }
func (e *hostEchoTool) Schema() api.ToolSchema {
	return api.ToolSchema{Name: "echo"}
}
func (e *hostEchoTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	msg, _ := call.Arguments["msg"].(string)
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: "echo: " + msg,
	}, nil
}

type hostAnotherTool struct{}

func (e *hostAnotherTool) Name() string        { return "another" }
func (e *hostAnotherTool) Description() string { return "another test tool" }
func (e *hostAnotherTool) Kind() tools.Kind    { return tools.KindNative }
func (e *hostAnotherTool) Schema() api.ToolSchema {
	return api.ToolSchema{Name: "another"}
}
func (e *hostAnotherTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: "another",
	}, nil
}

type hostNamedTool struct {
	name string
}

func (t *hostNamedTool) Name() string        { return t.name }
func (t *hostNamedTool) Description() string { return t.name + " test tool" }
func (t *hostNamedTool) Kind() tools.Kind    { return tools.KindNative }
func (t *hostNamedTool) Schema() api.ToolSchema {
	return api.ToolSchema{Name: t.name}
}
func (t *hostNamedTool) Execute(_ context.Context, call api.ToolCall) (*api.ToolResult, error) {
	return &api.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Content: t.name,
	}, nil
}

func containsMessage(messages []api.Message, role, content string) bool {
	for _, msg := range messages {
		if msg.Role == role && msg.Content == content {
			return true
		}
	}
	return false
}

func containsMessageContains(messages []api.Message, role, content string) bool {
	for _, msg := range messages {
		if msg.Role == role && strings.Contains(msg.Content, content) {
			return true
		}
	}
	return false
}

func toolSchemaListed(schemas []api.ToolSchema, name string) bool {
	for _, schema := range schemas {
		if schema.Name == name {
			return true
		}
	}
	return false
}

func containsText(text, fragment string) bool {
	return strings.Contains(text, fragment)
}

type summaryFromContextProvider struct{}

func (p *summaryFromContextProvider) Name() string { return "summary-from-context" }

func (p *summaryFromContextProvider) Generate(_ context.Context, req *api.GenerateRequest) (*api.GenerateResult, error) {
	content := "baseline summary without recalled memory"
	if containsMessageContains(req.Messages, "system", "always mention migration checklist in final summary") {
		content = "final summary uses recalled memory"
	}
	return &api.GenerateResult{
		Content:      content,
		FinishReason: api.FinishFinal,
	}, nil
}
