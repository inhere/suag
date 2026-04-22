import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { RunInspector } from "./run-inspector";

it("renders compact session summary and run details panels by default", () => {
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
        latestMessagePreview: "done",
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
      externalTasks={[]}
      externalActivities={[]}
      externalMessages={[]}
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

  expect(screen.getByRole("heading", { name: "Session summary" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Run details" })).toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Focused Run" })).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "External Messages" })).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "External Activities" })).not.toBeInTheDocument();
  expect(screen.getByText("已完成基础方案")).toBeInTheDocument();
});

it("still prefers failure reason for failed focused runs in compact inspector", () => {
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
          latestMessageText: "tool call failed: bad payload",
          summary: "summary should not be first",
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

  expect(screen.getByText("tool call failed: bad payload")).toBeInTheDocument();
  expect(screen.queryByText("summary should not be first")).not.toBeInTheDocument();
});
