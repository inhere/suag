import { describe, expect, it } from "vitest";
import type { SessionSummary, StateSnapshotV0 } from "../../lib/host-types";
import {
  emptySessionViewState,
  hydrateFromSnapshot,
  reduceSessionMessage,
  type SessionListItem,
  type SessionViewState,
} from "./session-store";

function makeSnapshot(
  overrides: Partial<StateSnapshotV0> = {},
): StateSnapshotV0 {
  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt: "2026-04-12T09:00:00Z",
    threadId: "sess_001",
    sessionId: "sess_001",
    title: "host runtime",
    status: "RUNNING",
    turnId: "turn_002",
    lastTurnId: "turn_002",
    currentRunId: "run_002",
    currentAgentId: "default",
    latestEventType: "run.started",
    eventCount: 4,
    turns: [
      {
        turnId: "turn_002",
        sessionId: "sess_001",
        primaryRunId: "run_002",
        currentRunId: "run_002",
        latestRunId: "run_002",
        latestAgentId: "default",
        displayState: "running",
        latestEventType: "run.started",
        latestMessagePreview: "正在整理页面结构",
        eventCount: 4,
        runCount: 1,
        activeRunCount: 1,
        completedRunCount: 0,
        failedRunCount: 0,
        activeRunIds: ["run_002"],
        completedRunIds: [],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-12T09:00:00Z",
      },
    ],
    runs: [
      {
        runId: "run_002",
        turnId: "turn_002",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "running",
        latestEventType: "run.started",
        latestMessageId: "msg_002",
        latestMessageText: "正在整理页面结构",
        summary: "",
        externalTaskIds: [],
        eventCount: 4,
        updatedAt: "2026-04-12T09:00:00Z",
      },
    ],
    externalTasks: [],
    externalActivities: [],
    externalMessages: [],
    events: [],
    ...overrides,
  };
}

function makeSessionItem(
  overrides: Partial<SessionListItem> = {},
): SessionListItem {
  return {
    sessionId: "sess_001",
    title: "host runtime",
    status: "RUNNING",
    updatedAt: "2026-04-12T09:00:00Z",
    preview: "正在整理页面结构",
    ...overrides,
  };
}

describe("session-store", () => {
  it("hydrates the workbench from STATE_SNAPSHOT v0 and keeps current active run focused", () => {
    const state = reduceSessionMessage(emptySessionViewState, {
      type: "STATE_SNAPSHOT",
      snapshot: makeSnapshot({ currentRunId: "run_002" }),
    });

    expect(state.currentSessionId).toBe("sess_001");
    expect(state.snapshot?.currentRunId).toBe("run_002");
    expect(state.focusedRunId).toBe("run_002");
  });

  it("falls back to the next available active run if the stored focus no longer exists", () => {
    const state = hydrateFromSnapshot(
      {
        ...emptySessionViewState,
        currentSessionId: "sess_001",
        focusedRunId: "run_missing",
      },
      makeSnapshot({
        currentRunId: "",
        runs: [
          {
            runId: "run_101",
            turnId: "turn_002",
            sessionId: "sess_001",
            agentId: "reviewer",
            displayState: "external_running",
            latestEventType: "run.started",
            latestMessageId: "msg_101",
            latestMessageText: "reviewing",
            summary: "",
            externalTaskIds: [],
            eventCount: 1,
            updatedAt: "2026-04-12T09:00:00Z",
          },
        ],
      }),
    );

    expect(state.focusedRunId).toBe("run_101");
  });

  it("upserts the hydrated snapshot into recent sessions with fresh preview text", () => {
    const state: SessionViewState = {
      ...emptySessionViewState,
      recentSessions: [
        makeSessionItem({
          sessionId: "sess_001",
          title: "older title",
          preview: "older preview",
          updatedAt: "2026-04-12T08:00:00Z",
        }),
      ],
    };

    const next = hydrateFromSnapshot(state, makeSnapshot());

    expect(next.recentSessions).toEqual([
      {
        sessionId: "sess_001",
        title: "host runtime",
        status: "RUNNING",
        updatedAt: "2026-04-12T09:00:00Z",
        preview: "正在整理页面结构",
      },
    ]);
  });

  it("merges incremental text updates into the hydrated snapshot", () => {
    const next = reduceSessionMessage(
      hydrateFromSnapshot(emptySessionViewState, makeSnapshot()),
      {
        type: "TEXT_MESSAGE_CONTENT",
        messageId: "msg_002",
        delta: " done",
      },
    );

    expect(next.snapshot?.runs[0]?.latestMessageText).toBe("正在整理页面结构 done");
    expect(next.snapshot?.turns[0]?.latestMessagePreview).toBe("正在整理页面结构 done");
    expect(next.snapshot?.externalMessages[0]?.content).toBe("正在整理页面结构 done");
  });
});
