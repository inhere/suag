import { describe, expect, it } from "vitest";
import type { StateSnapshotV0 } from "../../lib/host-types";
import {
  selectActiveRuns,
  selectCurrentTurn,
  selectFocusedRun,
  selectRecentTurns,
  selectSessionPreview,
} from "./session-selectors";

function makeSnapshot(
  overrides: Partial<StateSnapshotV0> = {},
): StateSnapshotV0 {
  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt: "2026-04-12T08:00:00Z",
    threadId: "sess_001",
    sessionId: "sess_001",
    title: "demo",
    status: "RUNNING",
    turnId: "turn_002",
    lastTurnId: "turn_002",
    currentRunId: "run_002",
    currentAgentId: "reviewer",
    latestEventType: "run.finished",
    eventCount: 8,
    turns: [
      {
        turnId: "turn_001",
        sessionId: "sess_001",
        primaryRunId: "run_001",
        currentRunId: "run_001",
        latestRunId: "run_001",
        latestAgentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessagePreview: "first turn done",
        eventCount: 3,
        runCount: 1,
        activeRunCount: 0,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: [],
        completedRunIds: ["run_001"],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-12T07:55:00Z",
      },
      {
        turnId: "turn_002",
        sessionId: "sess_001",
        primaryRunId: "run_002",
        currentRunId: "run_002",
        latestRunId: "run_003",
        latestAgentId: "reviewer",
        displayState: "external_running",
        latestEventType: "run.started",
        latestMessagePreview: "review in progress",
        eventCount: 5,
        runCount: 2,
        activeRunCount: 1,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: ["run_002"],
        completedRunIds: ["run_003"],
        failedRunIds: [],
        hasExternalWork: true,
        updatedAt: "2026-04-12T08:00:00Z",
      },
    ],
    runs: [
      {
        runId: "run_001",
        turnId: "turn_001",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessageId: "msg_001",
        latestMessageText: "turn one complete",
        summary: "turn one complete",
        externalTaskIds: [],
        eventCount: 3,
        updatedAt: "2026-04-12T07:55:00Z",
      },
      {
        runId: "run_002",
        turnId: "turn_002",
        sessionId: "sess_001",
        agentId: "reviewer",
        displayState: "external_running",
        latestEventType: "run.started",
        latestMessageId: "msg_002",
        latestMessageText: "review in progress",
        summary: "",
        externalTaskIds: ["ext_001"],
        eventCount: 2,
        updatedAt: "2026-04-12T08:00:00Z",
      },
      {
        runId: "run_003",
        turnId: "turn_002",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessageId: "msg_003",
        latestMessageText: "summary text",
        summary: "final summary",
        externalTaskIds: [],
        eventCount: 3,
        updatedAt: "2026-04-12T07:59:00Z",
      },
    ],
    externalTasks: [],
    externalActivities: [],
    externalMessages: [],
    events: [],
    ...overrides,
  };
}

describe("session-selectors", () => {
  it("selects the current active run from the snapshot", () => {
    const snapshot = makeSnapshot();

    expect(selectFocusedRun(snapshot, null)?.runId).toBe("run_002");
  });

  it("prefers an explicitly focused run when it exists", () => {
    const snapshot = makeSnapshot();

    expect(selectFocusedRun(snapshot, "run_003")?.runId).toBe("run_003");
  });

  it("selects the current turn by snapshot turnId", () => {
    const snapshot = makeSnapshot();

    expect(selectCurrentTurn(snapshot)?.turnId).toBe("turn_002");
  });

  it("returns only active runs for the active run list", () => {
    const snapshot = makeSnapshot();

    expect(selectActiveRuns(snapshot).map((run) => run.runId)).toEqual(["run_002"]);
  });

  it("builds preview text from the current turn before falling back to runs", () => {
    const snapshot = makeSnapshot();

    expect(selectSessionPreview(snapshot)).toBe("review in progress");
  });

  it("returns recent turns ordered by updatedAt descending", () => {
    const snapshot = makeSnapshot({
      turns: [
        {
          turnId: "turn_001",
          sessionId: "sess_001",
          primaryRunId: "run_001",
          currentRunId: "run_001",
          latestRunId: "run_001",
          latestAgentId: "default",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessagePreview: "older turn",
          eventCount: 3,
          runCount: 1,
          activeRunCount: 0,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: [],
          completedRunIds: ["run_001"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T07:55:00Z",
        },
        {
          turnId: "turn_003",
          sessionId: "sess_001",
          primaryRunId: "run_004",
          currentRunId: "run_004",
          latestRunId: "run_004",
          latestAgentId: "planner",
          displayState: "waiting",
          latestEventType: "turn.waiting",
          latestMessagePreview: "newer turn",
          eventCount: 1,
          runCount: 1,
          activeRunCount: 1,
          completedRunCount: 0,
          failedRunCount: 0,
          activeRunIds: ["run_004"],
          completedRunIds: [],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T08:05:00Z",
        },
      ],
    });

    expect(selectRecentTurns(snapshot, 2).map((turn) => turn.turnId)).toEqual([
      "turn_003",
      "turn_001",
    ]);
  });
});
