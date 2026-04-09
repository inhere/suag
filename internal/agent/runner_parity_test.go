package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/inhere/axon/internal/agent"
	"github.com/inhere/axon/internal/agentcorestorage"
	"github.com/inhere/axon/internal/hitl"
	"github.com/inhere/axon/internal/llm"
	"github.com/inhere/axon/internal/memory"
	"github.com/inhere/axon/internal/tracing"
	"github.com/inhere/axon/pkg/agentcore"
)

type parityRunOutcome struct {
	result *agent.TaskResult
	err    error
}

func TestRunnerParity_DirectAnswer(t *testing.T) {
	reactMock := llm.NewMockProvider("react").QueueText("The answer is 42.")
	coreMock := llm.NewMockProvider("core").QueueText("The answer is 42.")

	reactAgent := newTestAgent(reactMock, newTestRegistry())
	coreAgent := newTestAgent(coreMock, newTestRegistry())

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunner(reactAgent, 20, tracing.NewInMemoryTracer(nil), nil)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 20,
	})

	task := newTask("What is the answer?")
	reactResult, err := reactRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("react Run() error: %v", err)
	}
	coreResult, err := coreRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("agentcore Run() error: %v", err)
	}

	if reactResult.Output != coreResult.Output {
		t.Fatalf("output mismatch: react=%q agentcore=%q", reactResult.Output, coreResult.Output)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
	assertMemoryTranscriptEqual(t, reactAgent, coreAgent)
}

func TestRunnerParity_OneToolCall(t *testing.T) {
	reactMock := llm.NewMockProvider("react").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("Tool returned echo: hello. Task done.")
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("echo", map[string]any{"msg": "hello"}).
		QueueText("Tool returned echo: hello. Task done.")

	reg := newTestRegistry(&coreEchoTool{})
	reactAgent := newTestAgent(reactMock, reg)
	coreAgent := newTestAgent(coreMock, reg)

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunner(reactAgent, 20, tracing.NewInMemoryTracer(nil), nil)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 20,
	})

	task := newTask("Echo hello then finish")
	reactResult, err := reactRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("react Run() error: %v", err)
	}
	coreResult, err := coreRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("agentcore Run() error: %v", err)
	}

	if reactResult.Output != coreResult.Output {
		t.Fatalf("output mismatch: react=%q agentcore=%q", reactResult.Output, coreResult.Output)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
	assertMemoryTranscriptEqual(t, reactAgent, coreAgent)
}

func TestRunnerParity_MultiStepToolChain(t *testing.T) {
	reactMock := llm.NewMockProvider("react").
		QueueToolCall("echo", map[string]any{"msg": "step1"}).
		QueueToolCall("echo", map[string]any{"msg": "step2"}).
		QueueToolCall("echo", map[string]any{"msg": "step3"}).
		QueueText("All three steps done.")
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("echo", map[string]any{"msg": "step1"}).
		QueueToolCall("echo", map[string]any{"msg": "step2"}).
		QueueToolCall("echo", map[string]any{"msg": "step3"}).
		QueueText("All three steps done.")

	reg := newTestRegistry(&coreEchoTool{})
	reactAgent := newTestAgent(reactMock, reg)
	coreAgent := newTestAgent(coreMock, reg)

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunner(reactAgent, 20, tracing.NewInMemoryTracer(nil), nil)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 20,
	})

	task := newTask("Run three echo steps")
	reactResult, err := reactRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("react Run() error: %v", err)
	}
	coreResult, err := coreRunner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("agentcore Run() error: %v", err)
	}

	if reactResult.Output != coreResult.Output {
		t.Fatalf("output mismatch: react=%q agentcore=%q", reactResult.Output, coreResult.Output)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
	if reactMock.CallCount() != coreMock.CallCount() {
		t.Fatalf("llm call count mismatch: react=%d agentcore=%d", reactMock.CallCount(), coreMock.CallCount())
	}
	assertMemoryTranscriptEqual(t, reactAgent, coreAgent)
}

func TestRunnerParity_HITLContinue(t *testing.T) {
	reactFailTool := &failTool{}
	coreFailTool := &failTool{}
	reactMock := llm.NewMockProvider("react").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueText("Recovered after human intervention.")
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueText("Recovered after human intervention.")

	reactAgent := newTestAgent(reactMock, newTestRegistry(reactFailTool))
	coreAgent := newTestAgent(coreMock, newTestRegistry(coreFailTool))

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactHITL := hitl.NewInMemoryManager(nil)
	coreHITL := hitl.NewInMemoryManager(nil)
	reactRunner := agent.NewReactRunner(reactAgent, 20, tracing.NewInMemoryTracer(nil), reactHITL)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:       store,
		HITL:          coreHITL,
		FailThreshold: 3,
		MaxSteps:      20,
	})

	reactCh := make(chan parityRunOutcome, 1)
	coreCh := make(chan parityRunOutcome, 1)
	go func() {
		r, runErr := reactRunner.Run(context.Background(), newTask("Use fail_tool"))
		reactCh <- parityRunOutcome{result: r, err: runErr}
	}()
	go func() {
		r, runErr := coreRunner.Run(context.Background(), newTask("Use fail_tool"))
		coreCh <- parityRunOutcome{result: r, err: runErr}
	}()

	reactTicket := waitPendingTicket(t, reactHITL)
	coreTicket := waitPendingTicket(t, coreHITL)

	if err := reactHITL.Respond(reactTicket.ID, "Stop using fail_tool, just finish."); err != nil {
		t.Fatalf("react Respond error: %v", err)
	}
	if err := coreHITL.Respond(coreTicket.ID, "Stop using fail_tool, just finish."); err != nil {
		t.Fatalf("agentcore Respond error: %v", err)
	}

	reactOutcome := waitOutcome(t, reactCh)
	coreOutcome := waitOutcome(t, coreCh)
	if reactOutcome.err != nil {
		t.Fatalf("react Run() error after HITL: %v", reactOutcome.err)
	}
	if coreOutcome.err != nil {
		t.Fatalf("agentcore Run() error after HITL: %v", coreOutcome.err)
	}
	if reactOutcome.result.Output != coreOutcome.result.Output {
		t.Fatalf("output mismatch: react=%q agentcore=%q", reactOutcome.result.Output, coreOutcome.result.Output)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
	assertMemoryTranscriptEqual(t, reactAgent, coreAgent)
}

func TestRunnerParity_HITLDismissed(t *testing.T) {
	reactMock := llm.NewMockProvider("react").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil)
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil).
		QueueToolCall("fail_tool", nil)

	reactAgent := newTestAgent(reactMock, newTestRegistry(&failTool{}))
	coreAgent := newTestAgent(coreMock, newTestRegistry(&failTool{}))

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactHITL := hitl.NewInMemoryManager(nil)
	coreHITL := hitl.NewInMemoryManager(nil)
	reactRunner := agent.NewReactRunner(reactAgent, 20, tracing.NewInMemoryTracer(nil), reactHITL)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:       store,
		HITL:          coreHITL,
		FailThreshold: 3,
		MaxSteps:      20,
	})

	reactErrCh := make(chan error, 1)
	coreErrCh := make(chan error, 1)
	go func() {
		_, runErr := reactRunner.Run(context.Background(), newTask("fail"))
		reactErrCh <- runErr
	}()
	go func() {
		_, runErr := coreRunner.Run(context.Background(), newTask("fail"))
		coreErrCh <- runErr
	}()

	reactTicket := waitPendingTicket(t, reactHITL)
	coreTicket := waitPendingTicket(t, coreHITL)

	if err := reactHITL.Dismiss(reactTicket.ID, "task is unrecoverable"); err != nil {
		t.Fatalf("react Dismiss error: %v", err)
	}
	if err := coreHITL.Dismiss(coreTicket.ID, "task is unrecoverable"); err != nil {
		t.Fatalf("agentcore Dismiss error: %v", err)
	}

	reactErr := waitError(t, reactErrCh)
	coreErr := waitError(t, coreErrCh)
	if reactErr == nil || coreErr == nil {
		t.Fatalf("expected both runners to fail after dismissal, got react=%v agentcore=%v", reactErr, coreErr)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
}

func TestRunnerParity_MaxStepsExceeded(t *testing.T) {
	reactMock := llm.NewMockProvider("react")
	coreMock := llm.NewMockProvider("core")
	for range 10 {
		reactMock.QueueToolCall("echo", map[string]any{"msg": "loop"})
		coreMock.QueueToolCall("echo", map[string]any{"msg": "loop"})
	}

	reg := newTestRegistry(&coreEchoTool{})
	reactAgent := newTestAgent(reactMock, reg)
	coreAgent := newTestAgent(coreMock, reg)

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunner(reactAgent, 3, tracing.NewInMemoryTracer(nil), nil)
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:  store,
		MaxSteps: 3,
	})

	_, reactErr := reactRunner.Run(context.Background(), newTask("loop forever"))
	_, coreErr := coreRunner.Run(context.Background(), newTask("loop forever"))
	if reactErr == nil || coreErr == nil {
		t.Fatalf("expected both runners to fail on max steps, got react=%v agentcore=%v", reactErr, coreErr)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
}

func TestRunnerParity_ToolPolicyEscalatesFailure(t *testing.T) {
	reactMock := llm.NewMockProvider("react").
		QueueToolCall("fail_tool", nil)
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("fail_tool", nil)

	reactAgent := newTestAgent(reactMock, newTestRegistry(&failTool{}))
	coreAgent := newTestAgent(coreMock, newTestRegistry(&failTool{}))

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunnerWithConfig(reactAgent, agent.ReactRunnerConfig{
		MaxSteps:            5,
		Tracer:              tracing.NewInMemoryTracer(nil),
		ToolExecutionPolicy: failOnToolErrorPolicy{},
	})
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:             store,
		MaxSteps:            5,
		ToolExecutionPolicy: failOnToolErrorPolicy{},
	})

	_, reactErr := reactRunner.Run(context.Background(), newTask("tool policy parity"))
	_, coreErr := coreRunner.Run(context.Background(), newTask("tool policy parity"))
	if reactErr == nil || coreErr == nil {
		t.Fatalf("expected both runners to fail, got react=%v agentcore=%v", reactErr, coreErr)
	}
	if reactErr.Error() != coreErr.Error() {
		t.Fatalf("error mismatch: react=%v agentcore=%v", reactErr, coreErr)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
}

func TestRunnerParity_ErrorPolicyYieldsRecoverableFailure(t *testing.T) {
	reactMock := llm.NewMockProvider("react").QueueError(context.Canceled)
	coreMock := llm.NewMockProvider("core").QueueError(context.Canceled)

	reactAgent := newTestAgent(reactMock, newTestRegistry())
	coreAgent := newTestAgent(coreMock, newTestRegistry())

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactRunner := agent.NewReactRunnerWithConfig(reactAgent, agent.ReactRunnerConfig{
		MaxSteps:    5,
		Tracer:      tracing.NewInMemoryTracer(nil),
		ErrorPolicy: yieldOnRecoverableErrorPolicy{},
	})
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:     store,
		MaxSteps:    5,
		ErrorPolicy: yieldOnRecoverableErrorPolicy{},
	})

	reactResult, reactErr := reactRunner.Run(context.Background(), newTask("error policy parity"))
	coreResult, coreErr := coreRunner.Run(context.Background(), newTask("error policy parity"))
	if reactErr != nil || coreErr != nil {
		t.Fatalf("expected both runners to yield via error policy, got react=%v agentcore=%v", reactErr, coreErr)
	}
	if reactResult == nil || coreResult == nil {
		t.Fatalf("expected non-nil yielded results, got react=%#v agentcore=%#v", reactResult, coreResult)
	}
	if reactResult.Summary != coreResult.Summary {
		t.Fatalf("summary mismatch: react=%q agentcore=%q", reactResult.Summary, coreResult.Summary)
	}
	if reactRunner.GetState() != coreRunner.GetState() {
		t.Fatalf("state mismatch: react=%s agentcore=%s", reactRunner.GetState(), coreRunner.GetState())
	}
}

func TestRunnerParity_ResumePolicySuppressesResumeInstruction(t *testing.T) {
	const resumeInstruction = "do not append this instruction"

	reactMock := llm.NewMockProvider("react").QueueText("react done")
	coreMock := llm.NewMockProvider("core").
		QueueToolCall("echo", map[string]any{"msg": "persisted"}).
		QueueText("core done")

	reactAgent := newTestAgent(reactMock, newTestRegistry())
	coreAgent := newTestAgent(coreMock, newTestRegistry(&coreEchoTool{}))

	store, err := agentcorestorage.NewSQLiteStorage(t.TempDir() + "/agentcore.db")
	if err != nil {
		t.Fatalf("expected sqlite storage to initialize, got %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	reactAgent.State = agent.AgentRunning
	reactRunner := agent.NewReactRunnerWithConfig(reactAgent, agent.ReactRunnerConfig{
		MaxSteps:     5,
		Tracer:       tracing.NewInMemoryTracer(nil),
		ResumePolicy: suppressResumeInstructionPolicy{},
	})
	if err := reactRunner.Suspend("pause"); err != nil {
		t.Fatalf("react Suspend() error: %v", err)
	}
	if err := reactRunner.Resume(resumeInstruction); err != nil {
		t.Fatalf("react Resume() error: %v", err)
	}
	if reactMessages := reactAgent.Memory.GetMessages(); containsUserInstruction(reactMessages, resumeInstruction) {
		t.Fatalf("react memory should not include suppressed resume instruction: %#v", reactMessages)
	}

	hook := &yieldBeforeSecondModelAgentHook{}
	coreRunner := agent.NewAgentcoreRunner(coreAgent, agent.AgentcoreRunnerConfig{
		Storage:      store,
		MaxSteps:     5,
		ResumePolicy: suppressResumeInstructionPolicy{},
		Hooks:        []agentcore.Hook{hook},
	})

	firstCoreResult, err := coreRunner.Run(context.Background(), newTask("core resume parity"))
	if err != nil {
		t.Fatalf("agentcore first Run() error: %v", err)
	}
	if firstCoreResult == nil || firstCoreResult.Summary != "await_resume" {
		t.Fatalf("expected agentcore yield before resume, got %#v", firstCoreResult)
	}
	if err := coreRunner.Resume(resumeInstruction); err != nil {
		t.Fatalf("agentcore Resume() error: %v", err)
	}

	coreResult, err := coreRunner.Run(context.Background(), newTask("core resume parity"))
	if err != nil {
		t.Fatalf("agentcore resumed Run() error: %v", err)
	}
	if coreResult == nil || coreResult.Output != "core done" {
		t.Fatalf("unexpected agentcore resumed result: %#v", coreResult)
	}
	if coreMessages := coreAgent.Memory.GetMessages(); containsUserInstruction(coreMessages, resumeInstruction) {
		t.Fatalf("agentcore memory should not include suppressed resume instruction: %#v", coreMessages)
	}
	if reactRunner.GetState() != agent.AgentRunning {
		t.Fatalf("unexpected react state after Resume(): %s", reactRunner.GetState())
	}
	if coreRunner.GetState() != agent.AgentCompleted {
		t.Fatalf("unexpected agentcore state after resumed completion: %s", coreRunner.GetState())
	}
}

func assertMemoryTranscriptEqual(t *testing.T, reactAgent, coreAgent *agent.Agent) {
	t.Helper()

	reactMessages := reactAgent.Memory.GetMessages()
	coreMessages := coreAgent.Memory.GetMessages()
	if len(reactMessages) != len(coreMessages) {
		t.Fatalf("memory length mismatch: react=%d agentcore=%d", len(reactMessages), len(coreMessages))
	}
	for i := range reactMessages {
		if reactMessages[i].Role != coreMessages[i].Role || reactMessages[i].Content != coreMessages[i].Content {
			t.Fatalf(
				"memory message %d mismatch: react=%#v agentcore=%#v",
				i,
				reactMessages[i],
				coreMessages[i],
			)
		}
	}
}

func waitPendingTicket(t *testing.T, mgr hitl.Manager) *hitl.Ticket {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending := mgr.ListPending()
		if len(pending) > 0 {
			return pending[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HITL ticket was never created")
	return nil
}

func waitOutcome(t *testing.T, ch <-chan parityRunOutcome) parityRunOutcome {
	t.Helper()

	select {
	case outcome := <-ch:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for runner outcome")
		return parityRunOutcome{}
	}
}

func waitError(t *testing.T, ch <-chan error) error {
	t.Helper()

	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for runner error")
		return nil
	}
}

func containsUserInstruction(messages []memory.Message, instruction string) bool {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Content == instruction {
			return true
		}
	}
	return false
}
