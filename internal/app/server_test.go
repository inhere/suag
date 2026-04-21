package app

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	"github.com/inhere/axon/internal/host/gateway"
	"github.com/inhere/axon/internal/host/intrunner"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/ops/cron"
	"github.com/inhere/axon/internal/ops/hitl"
	"github.com/inhere/axon/internal/orchestrator"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
	agentcoreapi "github.com/inhere/axon/pkg/agentcore/api"
	_ "modernc.org/sqlite"
)

func TestNew_UsesDefaultCapabilityBuilderForRegistry(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	workspace := t.TempDir()
	_ = gkconfig.Set("workspace", workspace)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.provider_name", "openai")
	_ = gkconfig.Set("llm.api_key", "test-key")
	_ = gkconfig.Set("llm.base_url", "http://127.0.0.1")
	_ = gkconfig.Set("serpapi_key", "search-key")

	app, err := New()
	assert.NoErr(t, err)
	assert.NotNil(t, app)
	assert.NotNil(t, app.capabilityManager)

	assert.NotNil(t, mustGetTool(t, app.registry, "file_read"))
	assert.NotNil(t, mustGetTool(t, app.registry, "file_write"))
	assert.NotNil(t, mustGetTool(t, app.registry, "file_list"))
	assert.NotNil(t, mustGetTool(t, app.registry, "shell_exec"))
	assert.NotNil(t, mustGetTool(t, app.registry, "web_search"))
}

func TestBuildDefaultCapabilities_ProducesSubsetUsableByAppDependents(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})
	set := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	resolved := built.Manager.ResolveTurnCapabilities(set)
	subset := built.Registry.Subset(resolved.ToolNames)

	for _, name := range resolved.ToolNames {
		_, err := subset.Get(name)
		assert.NoErr(t, err)
	}
}

func TestBuildSessionRunner_DefaultsToInternalRunnerWithoutACP(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})
	provider := llm.NewMockProvider("mock").QueueText("done")
	_ = gkconfig.Set("agentcore.run_root", filepath.Join(t.TempDir(), "agentcore"))
	_ = gkconfig.Set("host.memory.root", filepath.Join(t.TempDir(), "memory"))
	a := &App{
		provider:             provider,
		registry:             built.Registry,
		capabilityManager:    built.Manager,
		defaultCapabilitySet: built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{}),
		tracer:               tracing.NewInMemoryTracer(nil),
		workspace:            t.TempDir(),
	}

	runner, err := a.buildSessionRunner()
	assert.NoErr(t, err)
	assert.NotNil(t, runner)

	internalRunner, ok := runner.(*intrunner.SessionRunner)
	assert.True(t, ok)
	t.Cleanup(func() {
		assert.NoErr(t, internalRunner.Close())
	})

	_, err = internalRunner.Run(context.Background(), session.RuntimeRequest{
		SessionID:   "sess_runner_default_caps",
		TurnID:      "turn_001",
		RunID:       "run_001",
		AgentID:     "default",
		Instruction: "say hi",
	})
	assert.NoErr(t, err)

	calls := provider.Calls()
	assert.Eq(t, 1, len(calls))
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec"}, toolNamesFromSchemas(calls[0].Tools))
}

func TestRuntimeContextBuilder_UsesSessionCapabilityMetadata(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	defaultSet := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	a := &App{
		registry:             built.Registry,
		capabilityManager:    built.Manager,
		defaultCapabilitySet: defaultSet,
	}

	sessionBuilder := a.newSessionMetadataBuilder()
	assert.NotNil(t, sessionBuilder)
	metadata := sessionBuilder(session.SessionMetadataBuildInput{
		SessionID: "sess_ctx_builder",
		AgentID:   "default",
		Message:   "hello",
	})
	_, ok := metadata[runtimecaps.CapabilitySetSessionMetadataKey].(map[string]any)
	assert.True(t, ok)

	runtimeBuilder := a.newRuntimeContextBuilder()
	assert.NotNil(t, runtimeBuilder)
	contextMap := runtimeBuilder(session.RuntimeContextBuildInput{
		SessionID:       "sess_ctx_builder",
		AgentID:         "default",
		Message:         "hello",
		SessionMetadata: metadata,
	})

	names, ok := contextMap["resolved_tool_names"].([]string)
	assert.True(t, ok)
	assertToolNamesEq(t, []string{"file_read", "file_write", "file_list", "shell_exec"}, names)
	mode, ok := contextMap["resolved_tool_mode"].(string)
	assert.True(t, ok)
	assert.Eq(t, "auto", mode)
}

func TestRuntimeContextBuilder_DerivesReadOnlyToolModeFromMessage(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	built, err := buildDefaultCapabilities(t.TempDir())
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	defaultSet := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	a := &App{
		registry:             built.Registry,
		capabilityManager:    built.Manager,
		defaultCapabilitySet: defaultSet,
	}

	sessionBuilder := a.newSessionMetadataBuilder()
	metadata := sessionBuilder(session.SessionMetadataBuildInput{
		SessionID: "sess_ctx_ro",
		AgentID:   "default",
		Message:   "hello",
	})

	runtimeBuilder := a.newRuntimeContextBuilder()
	contextMap := runtimeBuilder(session.RuntimeContextBuildInput{
		SessionID:       "sess_ctx_ro",
		AgentID:         "default",
		Message:         "请查看并分析当前文件，不要修改",
		SessionMetadata: metadata,
	})

	names, ok := contextMap["resolved_tool_names"].([]string)
	assert.True(t, ok)
	assertToolNamesEq(t, []string{"file_read", "file_list"}, names)
	mode, ok := contextMap["resolved_tool_mode"].(string)
	assert.True(t, ok)
	assert.Eq(t, "read_only", mode)
}

func TestRuntimeContextBuilder_DerivesReadOnlyToolModeFromRecallMetadata(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	workspace := t.TempDir()
	memoryRoot := filepath.Join(t.TempDir(), "memory")
	err := os.MkdirAll(filepath.Join(memoryRoot, "memory"), 0o755)
	assert.NoErr(t, err)
	err = os.WriteFile(
		filepath.Join(memoryRoot, "memory", "MEMORY.md"),
		[]byte("# MEMORY\n\nUpdated: 2026-04-21T00:00:00Z\n\n- [turn_learning] 先查看相关文件并分析问题，不要直接修改\n"),
		0o644,
	)
	assert.NoErr(t, err)

	_ = gkconfig.Set("workspace", workspace)
	_ = gkconfig.Set("host.memory.root", memoryRoot)

	built, err := buildDefaultCapabilities(workspace)
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, built.Close())
	})

	defaultSet := built.Manager.BuildDefaultCapabilitySet(runtimecaps.BuildSetInput{})
	a := &App{
		registry:             built.Registry,
		capabilityManager:    built.Manager,
		defaultCapabilitySet: defaultSet,
	}

	sessionBuilder := a.newSessionMetadataBuilder()
	metadata := sessionBuilder(session.SessionMetadataBuildInput{
		SessionID: "sess_ctx_recall",
		AgentID:   "default",
		Message:   "hello",
	})

	runtimeBuilder := a.newRuntimeContextBuilder()
	contextMap := runtimeBuilder(session.RuntimeContextBuildInput{
		SessionID:       "sess_ctx_recall",
		AgentID:         "default",
		Message:         "继续处理一下",
		SessionMetadata: metadata,
	})

	names, ok := contextMap["resolved_tool_names"].([]string)
	assert.True(t, ok)
	assertToolNamesEq(t, []string{"file_read", "file_list"}, names)
	mode, ok := contextMap["resolved_tool_mode"].(string)
	assert.True(t, ok)
	assert.Eq(t, "read_only", mode)
}

func TestServerRouter_HealthEndpointReturnsOK(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)

	dbPath := filepath.Join(t.TempDir(), "server-session.db")
	applyAppSessionMigration(t, dbPath)
	_ = gkconfig.Set("database_url", dbPath)
	_ = gkconfig.Set("workspace", t.TempDir())
	_ = gkconfig.Set("agentcore.run_root", filepath.Join(t.TempDir(), "runtime"))
	_ = gkconfig.Set("host.memory.root", filepath.Join(t.TempDir(), "memory"))

	addr := reserveLocalAddr(t)
	_ = gkconfig.Set("server.addr", addr)

	provider := llm.NewMockProvider("mock")
	registry := tools.NewRegistry()
	tracer := tracing.NewInMemoryTracer(nil)
	bus := gateway.NewEventBus()
	hitlMgr := hitl.NewInMemoryManager(nil)
	orch := orchestrator.New(orchestrator.Config{
		LLM:              provider,
		Registry:         registry,
		Tracer:           tracer,
		HITL:             hitlMgr,
		Workspace:        t.TempDir(),
		AgentcoreRunRoot: t.TempDir(),
	})

	a := &App{
		provider:  provider,
		registry:  registry,
		orch:      orch,
		bus:       bus,
		hitlMgr:   hitlMgr,
		tracer:    tracer,
		scheduler: cron.NewScheduler(orch, provider, registry),
		workspace: t.TempDir(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.RunServer(ctx)
	}()

	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/health")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	assert.Eq(t, 200, resp.StatusCode)

	cancel()
	select {
	case runErr := <-errCh:
		assert.NoErr(t, runErr)
	case <-time.After(3 * time.Second):
		t.Fatal("RunServer() did not stop after context cancel")
	}
}

func newAppSessionStore(t *testing.T) *session.SQLiteStore {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app-session.db")
	applyAppSessionMigration(t, dbPath)

	store, err := session.NewSQLiteStore(dbPath)
	assert.NoErr(t, err)
	t.Cleanup(func() {
		assert.NoErr(t, store.Close())
	})
	return store
}

func applyAppSessionMigration(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	assert.NoErr(t, err)
	defer func() {
		assert.NoErr(t, db.Close())
	}()

	for _, upSQL := range readAppSessionMigrationUps(t) {
		_, err := db.Exec(upSQL)
		assert.NoErr(t, err)
	}
}

func readAppSessionMigrationUps(t *testing.T) []string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	migrationDir := filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
	entries, err := filepath.Glob(filepath.Join(migrationDir, "*.sql"))
	assert.NoErr(t, err)
	sort.Strings(entries)
	if len(entries) == 0 {
		t.Fatalf("no migration files found in %s", migrationDir)
	}

	ups := make([]string, 0, len(entries))
	for _, migrationPath := range entries {
		content, err := os.ReadFile(migrationPath)
		assert.NoErr(t, err)

		parts := strings.Split(string(content), "-- Migrate:DOWN")
		if len(parts) != 2 {
			t.Fatalf("unexpected migration format in %s", migrationPath)
		}

		upSQL := strings.TrimSpace(strings.TrimPrefix(parts[0], "-- Migrate:UP"))
		if upSQL == "" {
			t.Fatalf("empty migration up section in %s", migrationPath)
		}
		ups = append(ups, upSQL)
	}
	return ups
}

func reserveLocalAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoErr(t, err)
	addr := ln.Addr().String()
	assert.NoErr(t, ln.Close())
	return addr
}

func toolNamesFromSchemas(schemas []agentcoreapi.ToolSchema) []string {
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Name)
	}
	return names
}
