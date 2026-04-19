import { render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { RunInspector } from "./run-inspector";

it("shows summary first for a completed focused run", () => {
  render(
    <RunInspector
      currentTurn={{
        turnId: "turn_002",
        sessionId: "sess_001",
        primaryRunId: "run_001",
        currentRunId: "run_001",
        latestRunId: "run_001",
        latestAgentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessagePreview: "已完成基础方案",
        eventCount: 3,
        runCount: 1,
        activeRunCount: 0,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: [],
        completedRunIds: ["run_001"],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-12T10:30:00Z",
      }}
      runs={[
        {
          runId: "run_001",
          turnId: "turn_002",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "msg_001",
          latestMessageText: "older text",
          summary: "已完成基础方案",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 3,
          updatedAt: "2026-04-12T10:30:00Z",
        },
      ]}
      externalTasks={[
        {
          externalTaskId: "ext_001",
          sessionId: "sess_001",
          turnId: "turn_002",
          runId: "run_001",
          adapter: "codex",
          remoteAgentName: "codex-reviewer",
          remoteSessionId: "remote_001",
          remoteTaskId: "task_001",
          state: "completed",
          lastHeartbeatAt: "2026-04-12T10:30:00Z",
          resultSummary: "任务已完成",
          metadata: {},
        },
      ]}
      externalActivities={[
        {
          externalTaskId: "ext_001",
          sessionId: "sess_001",
          turnId: "turn_002",
          runId: "run_001",
          agentId: "default",
          activityId: "act_001",
          kind: "delegate",
          status: "completed",
          title: "delegate",
          detail: "completed",
          startedAt: "2026-04-12T10:29:00Z",
          updatedAt: "2026-04-12T10:30:00Z",
        },
      ]}
      externalMessages={[
        {
          externalTaskId: "ext_001",
          messageId: "msg_001",
          sessionId: "sess_001",
          turnId: "turn_002",
          runId: "run_001",
          agentId: "default",
          content: "详细流式正文",
          started: true,
          completed: true,
          lastEventType: "run.external.message.end",
          lastSequence: 3,
          updatedAt: "2026-04-12T10:30:00Z",
        },
      ]}
      focusedRunId="run_001"
      focusedRunDetailStatus="loaded"
      onFocusRun={vi.fn()}
      onRetryFocusedRunDetail={vi.fn()}
      canStop={false}
      onStop={vi.fn()}
      canAddHint={false}
      onAddHint={vi.fn()}
      onOpenDebug={vi.fn()}
    />,
  );

  const focusedRunHeading = screen.getByRole("heading", { name: "Focused Run" });
  const focusedRunPanel = focusedRunHeading.closest("section");

  expect(focusedRunPanel).not.toBeNull();
  expect(within(focusedRunPanel as HTMLElement).getByRole("heading", { name: "Summary" })).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByRole("heading", { name: "Run Meta" })).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByRole("heading", { name: "External Messages" })).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByRole("heading", { name: "External Activities" })).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByText("已完成基础方案")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).queryByText("older text")).not.toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByText("详细流式正文")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByText("delegate: completed")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByText("run_001")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getAllByText("completed").length).toBeGreaterThan(0);
  expect(screen.getByText("codex-reviewer")).toBeInTheDocument();
});

it("shows loading and retry feedback for focused run detail states", () => {
  const onRetryFocusedRunDetail = vi.fn();
  const { rerender } = render(
    <RunInspector
      currentTurn={null}
      runs={[
        {
          runId: "run_010",
          turnId: "turn_010",
          sessionId: "sess_010",
          agentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "",
          latestMessageText: "",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 1,
          updatedAt: "2026-04-12T10:30:00Z",
        },
      ]}
      externalTasks={[]}
      externalActivities={[]}
      externalMessages={[]}
      focusedRunId="run_010"
      focusedRunDetailStatus="loading"
      onFocusRun={vi.fn()}
      onRetryFocusedRunDetail={onRetryFocusedRunDetail}
      canStop={false}
      onStop={vi.fn()}
      canAddHint={false}
      onAddHint={vi.fn()}
      onOpenDebug={vi.fn()}
    />,
  );

  expect(screen.getByText("Loading run detail...")).toBeInTheDocument();

  rerender(
    <RunInspector
      currentTurn={null}
      runs={[
        {
          runId: "run_010",
          turnId: "turn_010",
          sessionId: "sess_010",
          agentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "",
          latestMessageText: "",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 1,
          updatedAt: "2026-04-12T10:30:00Z",
        },
      ]}
      externalTasks={[]}
      externalActivities={[]}
      externalMessages={[]}
      focusedRunId="run_010"
      focusedRunDetailStatus="error"
      onFocusRun={vi.fn()}
      onRetryFocusedRunDetail={onRetryFocusedRunDetail}
      canStop={false}
      onStop={vi.fn()}
      canAddHint={false}
      onAddHint={vi.fn()}
      onOpenDebug={vi.fn()}
    />,
  );

  expect(screen.getByText("Failed to load run detail.")).toBeInTheDocument();
  screen.getByRole("button", { name: "Retry Detail" }).click();
  expect(onRetryFocusedRunDetail).toHaveBeenCalledTimes(1);
});

it("shows failed run error text in the focused summary when no summary exists", () => {
  render(
    <RunInspector
      currentTurn={null}
      runs={[
        {
          runId: "run_failed",
          turnId: "turn_failed",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "failed",
          latestEventType: "run.failed",
          latestMessageId: "",
          latestMessageText: "tool call \"file_read\" arguments must be a JSON object",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 2,
          updatedAt: "2026-04-18T10:00:01Z",
        },
      ]}
      externalTasks={[]}
      externalActivities={[]}
      externalMessages={[]}
      focusedRunId="run_failed"
      focusedRunDetailStatus="loaded"
      onFocusRun={vi.fn()}
      onRetryFocusedRunDetail={vi.fn()}
      canStop={false}
      onStop={vi.fn()}
      canAddHint={false}
      onAddHint={vi.fn()}
      onOpenDebug={vi.fn()}
    />,
  );

  const focusedRunHeading = screen.getByRole("heading", { name: "Focused Run" });
  const focusedRunPanel = focusedRunHeading.closest("section");
  expect(focusedRunPanel).not.toBeNull();
  expect(within(focusedRunPanel as HTMLElement).getByText("tool call \"file_read\" arguments must be a JSON object")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).getByText("failed")).toBeInTheDocument();
});

it("shows structured wait metadata for waiting runs", () => {
  render(
    <RunInspector
      currentTurn={null}
      runs={[
        {
          runId: "run_wait",
          turnId: "turn_wait",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "auth_required",
          latestEventType: "run.status",
          latestMessageId: "",
          latestMessageText: "waiting for approval",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {
            wait_kind: "auth_required",
            policy: "manual_review",
          },
          eventCount: 2,
          updatedAt: "2026-04-19T21:00:00Z",
        },
      ]}
      externalTasks={[]}
      externalActivities={[]}
      externalMessages={[]}
      focusedRunId="run_wait"
      focusedRunDetailStatus="loaded"
      onFocusRun={vi.fn()}
      onRetryFocusedRunDetail={vi.fn()}
      canStop={false}
      onStop={vi.fn()}
      canAddHint={false}
      onAddHint={vi.fn()}
      onOpenDebug={vi.fn()}
    />,
  );

  const waitInfoHeading = screen.getByRole("heading", { name: "Wait Info" });
  const waitInfoSection = waitInfoHeading.closest(".run-section");
  expect(waitInfoSection).not.toBeNull();
  expect(within(waitInfoSection as HTMLElement).getByText("wait_kind")).toBeInTheDocument();
  expect(within(waitInfoSection as HTMLElement).getByText("auth_required")).toBeInTheDocument();
  expect(within(waitInfoSection as HTMLElement).getByText("manual_review")).toBeInTheDocument();
});
