import { render, screen, within } from "@testing-library/react";
import { RunDetailsCompactPanel } from "./run-details-compact-panel";

it("shows runId, agent, status, and wait kind", () => {
  render(
    <RunDetailsCompactPanel
      focusedRunId="run_wait"
      runs={[
        {
          runId: "run_wait",
          turnId: "turn_001",
          sessionId: "sess_001",
          agentId: "planner",
          displayState: "auth_required",
          latestEventType: "run.waiting",
          latestMessageId: "msg_wait",
          latestMessageText: "waiting for approval",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {
            wait_kind: "auth_required",
          },
          eventCount: 2,
          updatedAt: "2026-04-21T10:00:00Z",
        },
      ]}
    />,
  );

  expect(screen.getByRole("heading", { name: "Run details" })).toBeInTheDocument();
  expect(screen.getByText("run_wait")).toBeInTheDocument();
  expect(screen.getByText("planner")).toBeInTheDocument();
  expect(screen.getAllByText("auth_required").length).toBeGreaterThan(0);
  expect(screen.queryByRole("heading", { name: "External Messages" })).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "External Activities" })).not.toBeInTheDocument();
});

it("prefers summary for completed runs", () => {
  render(
    <RunDetailsCompactPanel
      focusedRunId="run_done"
      runs={[
        {
          runId: "run_done",
          turnId: "turn_002",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "msg_001",
          latestMessageText: "older text",
          summary: "final summary text",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 3,
          updatedAt: "2026-04-21T10:00:00Z",
        },
      ]}
    />,
  );

  expect(screen.getByText("final summary text")).toBeInTheDocument();
  expect(screen.queryByText("older text")).not.toBeInTheDocument();
});

it("prefers failure reason for failed runs", () => {
  render(
    <RunDetailsCompactPanel
      focusedRunId="run_fail"
      runs={[
        {
          runId: "run_fail",
          turnId: "turn_003",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "failed",
          latestEventType: "run.failed",
          latestMessageId: "msg_002",
          latestMessageText: "tool failed: invalid payload",
          summary: "this summary should not be shown first",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 3,
          updatedAt: "2026-04-21T10:00:00Z",
        },
      ]}
    />,
  );

  expect(screen.getByText("tool failed: invalid payload")).toBeInTheDocument();
  expect(screen.queryByText("this summary should not be shown first")).not.toBeInTheDocument();
});

it("falls back to the first run and shows none when wait kind is missing", () => {
  render(
    <RunDetailsCompactPanel
      focusedRunId="run_missing"
      runs={[
        {
          runId: "run_first",
          turnId: "turn_004",
          sessionId: "sess_001",
          agentId: "default",
          displayState: "running",
          latestEventType: "run.started",
          latestMessageId: "msg_003",
          latestMessageText: "",
          summary: "",
          externalTaskIds: [],
          waitMetadata: {},
          eventCount: 1,
          updatedAt: "2026-04-21T10:05:00Z",
        },
      ]}
    />,
  );

  expect(screen.getByText("run_first")).toBeInTheDocument();
  expect(screen.getByText("default")).toBeInTheDocument();
  const statusLabel = screen.getByText("Status");
  const statusValue = statusLabel.closest(".context-panel__meta-item");
  expect(statusValue).not.toBeNull();
  expect(within(statusValue as HTMLElement).getByText("running")).toBeInTheDocument();
  expect(screen.getByText("none")).toBeInTheDocument();
  expect(screen.getByText("No run summary yet.")).toBeInTheDocument();
});
