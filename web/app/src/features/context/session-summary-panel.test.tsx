import { render, screen } from "@testing-library/react";
import { SessionSummaryPanel } from "./session-summary-panel";

it("shows status, turn, active runs, and updated time", () => {
  render(
    <SessionSummaryPanel
      currentTurn={{
        turnId: "turn_020",
        sessionId: "sess_001",
        primaryRunId: "run_020",
        currentRunId: "run_020",
        latestRunId: "run_020",
        latestAgentId: "default",
        displayState: "input_required",
        latestEventType: "run.waiting",
        latestMessagePreview: "waiting",
        eventCount: 4,
        runCount: 2,
        activeRunCount: 1,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: ["run_020"],
        completedRunIds: ["run_019"],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-21T11:02:03Z",
      }}
    />,
  );

  expect(screen.getByRole("heading", { name: "Session summary" })).toBeInTheDocument();
  expect(screen.getAllByText("input_required").length).toBeGreaterThan(0);
  expect(screen.getByText("turn_020")).toBeInTheDocument();
  expect(screen.getByText("1")).toBeInTheDocument();
  expect(screen.getByText("2026-04-21T11:02:03Z")).toBeInTheDocument();
});

it("shows fallback values when there is no current turn", () => {
  render(<SessionSummaryPanel currentTurn={null} />);

  expect(screen.getByRole("heading", { name: "Session summary" })).toBeInTheDocument();
  expect(screen.getAllByText("idle").length).toBeGreaterThan(0);
  expect(screen.getByText("none")).toBeInTheDocument();
  expect(screen.getByText("0")).toBeInTheDocument();
  expect(screen.getByText("-")).toBeInTheDocument();
});
