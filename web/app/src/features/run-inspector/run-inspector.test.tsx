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
      onFocusRun={vi.fn()}
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
