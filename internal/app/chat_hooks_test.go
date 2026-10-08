package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	gkconfig "github.com/gookit/config/v2"
	"github.com/inhere/suag/internal/data/testutil"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/internal/personal"
	"github.com/inhere/suag/internal/platform/tools"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/mock"
)

type hookTestProvider struct {
	mu    sync.Mutex
	calls [][]agentcore.Message
	count int
}

func (p *hookTestProvider) Name() string { return "hook-test" }
func (p *hookTestProvider) Generate(_ context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, append([]agentcore.Message(nil), req.Messages...))
	p.count++
	return &agentcore.GenerateResult{Content: fmt.Sprintf("answer-%d", p.count), FinishReason: agentcore.FinishFinal}, nil
}

type fakeHookClient struct {
	mu         sync.Mutex
	events     []goferHookEvent
	stopCount  int
	startCount chan struct{}
	eventWake  chan struct{}
	blockFirst bool
}

func newFakeHookClient() *fakeHookClient {
	return &fakeHookClient{startCount: make(chan struct{}, 8), eventWake: make(chan struct{}, 64), blockFirst: true}
}

func (f *fakeHookClient) record(event goferHookEvent) {
	f.mu.Lock()
	f.events = append(f.events, event)
	f.mu.Unlock()
	select {
	case f.eventWake <- struct{}{}:
	default:
	}
}

func (f *fakeHookClient) Call(_ context.Context, event goferHookEvent) goferHookResponse {
	f.record(event)
	if event.Name == "SessionStart" {
		return goferHookResponse{AdditionalContext: "startup-context"}
	}
	if event.Name == "UserPromptSubmit" {
		return goferHookResponse{AdditionalContext: "submit-context"}
	}
	return goferHookResponse{}
}

func (f *fakeHookClient) StartStop(_ context.Context, event goferHookEvent) chatHookWait {
	f.record(event)
	f.mu.Lock()
	f.stopCount++
	stopCount := f.stopCount
	f.mu.Unlock()
	f.startCount <- struct{}{}
	done := make(chan goferHookResult, 1)
	if stopCount == 1 && f.blockFirst {
		done <- goferHookResult{response: goferHookResponse{Decision: "block", Reason: "[gofer web 回复] continue"}}
	}
	return &fakeStopWait{done: done, interrupt: func() {
		interrupt := event
		interrupt.Name = "Interrupt"
		interrupt.LastAssistant = ""
		f.Call(context.Background(), interrupt)
	}}
}

func (f *fakeHookClient) snapshot() []goferHookEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]goferHookEvent(nil), f.events...)
}

type fakeStopWait struct {
	done      chan goferHookResult
	interrupt func()
}

func (w *fakeStopWait) Done() <-chan goferHookResult { return w.done }
func (w *fakeStopWait) Interrupt(context.Context)    { w.interrupt() }

func TestPersonalChatBlockStartsInjectedTurnAndMarksStopActive(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	t.Setenv("SUAG_HOME", home)
	provider := &hookTestProvider{}
	runner, _ := newCLITestRunner(t, provider)
	hooks := newFakeHookClient()
	reader, writer := io.Pipe()
	var stdout, stderr strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, reader, &stdout, &stderr, hooks)
	}()
	if _, err := io.WriteString(writer, "first prompt\n"); err != nil {
		t.Fatal(err)
	}
	waitHookEvent(t, hooks, "UserPromptSubmit", 2)
	if _, err := io.WriteString(writer, ":quit\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	events := hooks.snapshot()
	var prompts []goferHookEvent
	var stops []goferHookEvent
	for _, event := range events {
		if event.Name == "UserPromptSubmit" {
			prompts = append(prompts, event)
		}
		if event.Name == "Stop" {
			stops = append(stops, event)
		}
	}
	if len(prompts) != 2 || prompts[1].Prompt != "[gofer web 回复] continue" {
		t.Fatalf("expected block reason as a new submitted prompt, got %#v", prompts)
	}
	if prompts[0].TranscriptPath == "" {
		t.Fatal("SessionStart and prompt hooks should receive the transcript path")
	}
	if _, err := os.Stat(prompts[0].TranscriptPath); err != nil {
		t.Fatalf("transcript should exist before SessionStart: %v", err)
	}
	if len(stops) != 2 || !stops[1].StopHookActive {
		t.Fatalf("expected injected turn Stop to set stop_hook_active, got %#v", stops)
	}
	if !strings.Contains(stdout.String(), "[gofer web 回复] continue") {
		t.Fatalf("injected reply was not shown: %q", stdout.String())
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.calls) != 2 || !hasSystemText(provider.calls[0], "startup-context") || !hasSystemText(provider.calls[0], "submit-context") {
		t.Fatalf("hook context did not reach the next model request: %#v", provider.calls)
	}
}

func TestPersonalChatInterruptsStopBeforeNextPromptAndContinuesWithoutHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUAG_HOME", home)
	runner, _ := newCLITestRunner(t, &hookTestProvider{})
	hooks := newFakeHookClient()
	var stdout, stderr strings.Builder
	err := runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, strings.NewReader("first\nsecond\n:quit\n"), &stdout, &stderr, hooks)
	if err != nil {
		t.Fatal(err)
	}
	events := hooks.snapshot()
	firstStop, interrupt, secondPrompt := -1, -1, -1
	for i, event := range events {
		if firstStop < 0 && event.Name == "Stop" {
			firstStop = i
		}
		if firstStop >= 0 && interrupt < 0 && event.Name == "Interrupt" {
			interrupt = i
		}
		if interrupt >= 0 && secondPrompt < 0 && event.Name == "UserPromptSubmit" && event.Prompt == "second" {
			secondPrompt = i
		}
	}
	if firstStop < 0 || interrupt <= firstStop || secondPrompt <= interrupt {
		t.Fatalf("expected Stop, Interrupt, then next UserPromptSubmit; got %#v", events)
	}
	if err := runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, strings.NewReader(":quit\n"), &stdout, &stderr, nil); err != nil {
		t.Fatalf("missing hooks interrupted the conversation: %v", err)
	}
}

func TestPersonalChatInterruptsOnFirstByteBeforeLineSubmission(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_HOME", t.TempDir())
	runner, _ := newCLITestRunner(t, &hookTestProvider{})
	hooks := newFakeHookClient()
	hooks.blockFirst = false
	reader, writer := io.Pipe()
	defer writer.Close()
	var stdout, stderr strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, reader, &stdout, &stderr, hooks)
	}()
	if _, err := io.WriteString(writer, "first prompt\n"); err != nil {
		t.Fatal(err)
	}
	waitHookEvent(t, hooks, "Stop", 1)
	if _, err := io.WriteString(writer, "second prompt without enter"); err != nil {
		t.Fatal(err)
	}
	waitHookEvent(t, hooks, "Interrupt", 1)
	if countHookEvents(hooks.snapshot(), "UserPromptSubmit") != 1 {
		t.Fatalf("partial input was submitted before newline: %#v", hooks.snapshot())
	}
	if _, err := io.WriteString(writer, "\n"); err != nil {
		t.Fatal(err)
	}
	waitHookEvent(t, hooks, "UserPromptSubmit", 2)
	if _, err := io.WriteString(writer, ":quit\n"); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	prompts := make([]string, 0, 2)
	for _, event := range hooks.snapshot() {
		if event.Name == "UserPromptSubmit" {
			prompts = append(prompts, event.Prompt)
		}
	}
	if len(prompts) != 2 || prompts[1] != "second prompt without enter" {
		t.Fatalf("input should remain one intact prompt: %#v", prompts)
	}
}

func TestPersonalChatEOFExitsNormally(t *testing.T) {
	for _, input := range []string{"", "prompt\n"} {
		t.Run(fmt.Sprintf("input_%q", input), func(t *testing.T) {
			runner, _ := newCLITestRunner(t, mock.NewProvider(mock.Final("done")))
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := runPersonalChatWithHooks(ctx, runner, personalOptions{}, strings.NewReader(input), io.Discard, io.Discard, nil); err != nil {
				t.Fatalf("normal EOF returned an error: %v", err)
			}
		})
	}
}

func TestGoferHookFailuresAndJobEnvironmentAreBestEffort(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("GOFER_JOB_ID", "")
	t.Setenv("PATH", t.TempDir())
	_ = gkconfig.Set("gofer.hooks", "auto")
	_ = gkconfig.Set("gofer.command", "missing-gofer-command-7cb7")
	if got := newGoferHooks(); got != nil {
		t.Fatal("auto mode should skip a missing command")
	}

	t.Setenv("GOFER_JOB_ID", "inside-job")
	_ = gkconfig.Set("gofer.hooks", "on")
	if got := newGoferHooks(); got != nil {
		t.Fatal("hooks must be skipped inside a Gofer job")
	}

	// A configured but failing hook process must not prevent the chat turn.
	t.Setenv("GOFER_JOB_ID", "")
	runner, _ := newCLITestRunner(t, &hookTestProvider{})
	var stdout, stderr strings.Builder
	hooks := &goferHooks{command: "", agent: "suag", timeout: time.Millisecond, waitSec: 1}
	if err := runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, strings.NewReader("hello\n:quit\n"), &stdout, &stderr, hooks); err != nil {
		t.Fatalf("hook failure interrupted chat: %v", err)
	}
	if !strings.Contains(stdout.String(), "answer-1") {
		t.Fatalf("chat did not complete after hook errors: %q", stdout.String())
	}
}

func TestTranscriptsCanBeDisabledByConfig(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	if !transcriptsEnabled() {
		t.Fatal("transcripts should default to enabled")
	}
	_ = gkconfig.Set("transcripts", "off")
	if transcriptsEnabled() {
		t.Fatal("transcripts: off must disable transcript files")
	}
}

func TestHookPayloadCapsToolOutputAtUTF8Boundary(t *testing.T) {
	value := strings.Repeat("工", toolHookLimit)
	got := truncateHookOutput(value, toolHookLimit)
	if len(got) > toolHookLimit || !utf8.ValidString(got) {
		t.Fatalf("hook output is not valid bounded UTF-8: bytes=%d", len(got))
	}
}

func TestGoferStopInterruptIsSentBeforeStopProcessCancellation(t *testing.T) {
	var sequence []string
	done := make(chan goferHookResult, 1)
	done <- goferHookResult{}
	wait := &goferStopWait{
		call: func(_ context.Context, event goferHookEvent) goferHookResponse {
			if event.Name == "Interrupt" {
				sequence = append(sequence, "interrupt")
			}
			return goferHookResponse{}
		},
		event:  goferHookEvent{Name: "Stop", SessionID: "s1"},
		cancel: func() { sequence = append(sequence, "cancel") },
		done:   done,
	}
	wait.Interrupt(context.Background())
	if len(sequence) != 2 || sequence[0] != "interrupt" || sequence[1] != "cancel" {
		t.Fatalf("Stop process cancelled before Interrupt completed: %#v", sequence)
	}
}

func TestPersonalChatSendsOnlyBoundedRedactedShellOutputToPostToolUse(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	t.Setenv("SUAG_HOME", t.TempDir())
	repo, err := session.NewSQLiteRepository(testutil.NewMigratedSQLitePath(t, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	registry := tools.NewRegistry()
	registry.Register(appHookOutputTool{})
	runner, err := personal.New(personal.Config{
		Repository: repo,
		RunRoot:    t.TempDir(),
		Provider: mock.NewProvider(
			mock.ToolCall("call-1", "shell_exec", map[string]any{"command": "print environment"}),
			mock.Final("finished"),
		),
		Tools: registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks := newFakeHookClient()
	var stdout, stderr strings.Builder
	err = runPersonalChatWithHooks(context.Background(), runner, personalOptions{}, strings.NewReader("run command\n:quit\n"), &stdout, &stderr, hooks)
	if err != nil {
		t.Fatal(err)
	}
	var postTool *goferHookEvent
	postToolIndex, stopIndex := -1, -1
	for i, event := range hooks.snapshot() {
		if event.Name == "PostToolUse" {
			copyEvent := event
			postTool = &copyEvent
			postToolIndex = i
		}
		if event.Name == "Stop" {
			stopIndex = i
		}
	}
	if postTool == nil || postTool.ToolName != "shell_exec" || stopIndex <= postToolIndex {
		t.Fatalf("expected shell PostToolUse event, got %#v", hooks.snapshot())
	}
	if len(postTool.ToolOutput) > toolHookLimit || strings.Contains(postTool.ToolOutput, "canary-credential-4a2b") || !strings.Contains(postTool.ToolOutput, "[REDACTED]") {
		shown := postTool.ToolOutput[:min(len(postTool.ToolOutput), 80)]
		t.Fatalf("tool output was not bounded and redacted: bytes=%d output=%q", len(postTool.ToolOutput), shown)
	}
}

type appHookOutputTool struct{}

func (appHookOutputTool) Name() string        { return "shell_exec" }
func (appHookOutputTool) Description() string { return "test shell tool" }
func (appHookOutputTool) Schema() agentcore.ToolSchema {
	return agentcore.ToolSchema{Name: "shell_exec"}
}
func (appHookOutputTool) Kind() tools.Kind { return tools.KindNative }
func (appHookOutputTool) Execute(context.Context, agentcore.ToolCall) (*agentcore.ToolResult, error) {
	return &agentcore.ToolResult{Name: "shell_exec", Content: "Authorization: Bearer canary-credential-4a2b {\"api_key\":\"canary-credential-4a2b\"} " + strings.Repeat("x", toolHookLimit+100)}, nil
}

func waitHookEvent(t *testing.T, hooks *fakeHookClient, name string, count int) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		seen := 0
		for _, event := range hooks.snapshot() {
			if event.Name == name {
				seen++
			}
		}
		if seen >= count {
			return
		}
		select {
		case <-hooks.eventWake:
		case <-timer.C:
			t.Fatalf("timed out waiting for %s event %d", name, count)
		}
	}
}

func countHookEvents(events []goferHookEvent, name string) int {
	count := 0
	for _, event := range events {
		if event.Name == name {
			count++
		}
	}
	return count
}

func hasSystemText(messages []agentcore.Message, text string) bool {
	for _, message := range messages {
		if message.Role == "system" && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}
