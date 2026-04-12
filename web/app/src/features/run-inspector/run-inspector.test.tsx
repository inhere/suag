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
      externalTasks={[]}
      focusedRunId="run_001"
      onFocusRun={vi.fn()}
    />,
  );

  const focusedRunHeading = screen.getByRole("heading", { name: "Focused Run" });
  const focusedRunPanel = focusedRunHeading.closest("section");

  expect(focusedRunPanel).not.toBeNull();
  expect(within(focusedRunPanel as HTMLElement).getByText("已完成基础方案")).toBeInTheDocument();
  expect(within(focusedRunPanel as HTMLElement).queryByText("older text")).not.toBeInTheDocument();
});
