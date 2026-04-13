import { describe, expect, it } from "vitest";
import type {
  RunDetail,
  SessionDetail,
  SessionSummary,
  StateSnapshotV0,
} from "../../lib/host-types";
import {
  emptySessionViewState,
  hydrateFromSnapshot,
  mergeRunDetail,
  mergeSessionDetail,
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

function makeSessionDetail(
  overrides: Partial<SessionDetail> = {},
): SessionDetail {
  return {
    sessionId: "sess_001",
    title: "bootstrap title",
    status: "WAITING",
    lastTurnId: "turn_900",
    turns: [],
    runs: [],
    externalTasks: [
      {
        externalTaskId: "ext_bootstrap",
        sessionId: "sess_001",
        turnId: "turn_900",
        runId: "run_bootstrap",
        adapter: "codex",
        remoteAgentName: "bootstrap-codex",
        remoteSessionId: "remote_bootstrap_session",
        remoteTaskId: "remote_bootstrap_task",
        state: "running",
        lastHeartbeatAt: "2026-04-12T12:31:00Z",
        resultSummary: "",
        metadata: {},
      },
    ],
    ...overrides,
  } as SessionDetail;
}

function makeRunDetail(overrides: Partial<RunDetail> = {}): RunDetail {
  return {
    sessionId: "sess_001",
    turnId: "turn_hist",
    turn: {
      turnId: "turn_hist",
      sessionId: "sess_001",
      primaryRunId: "run_hist",
      currentRunId: "run_hist",
      latestRunId: "run_hist",
      latestAgentId: "reviewer",
      displayState: "completed",
      latestEventType: "run.finished",
      latestMessagePreview: "历史 run 摘要",
      eventCount: 8,
      runCount: 1,
      activeRunCount: 0,
      completedRunCount: 1,
      failedRunCount: 0,
      activeRunIds: [],
      completedRunIds: ["run_hist"],
      failedRunIds: [],
      hasExternalWork: true,
      updatedAt: "2026-04-12T14:00:00Z",
    },
    run: {
      runId: "run_hist",
      turnId: "turn_hist",
      sessionId: "sess_001",
      agentId: "reviewer",
      displayState: "completed",
      latestEventType: "run.finished",
      latestMessageId: "msg_hist",
      latestMessageText: "done",
      summary: "历史 run 摘要",
      externalTaskIds: ["ext_hist"],
      eventCount: 8,
      updatedAt: "2026-04-12T14:00:00Z",
    },
    externalActivities: [
      {
        externalTaskId: "ext_hist",
        sessionId: "sess_001",
        turnId: "turn_hist",
        runId: "run_hist",
        agentId: "reviewer",
        activityId: "run_hist:external",
        kind: "delegate",
        status: "completed",
        title: "delegate",
        detail: "completed",
        startedAt: "2026-04-12T13:59:00Z",
        updatedAt: "2026-04-12T14:00:00Z",
      },
    ],
    externalMessages: [
      {
        externalTaskId: "ext_hist",
        messageId: "msg_hist",
        sessionId: "sess_001",
        turnId: "turn_hist",
        runId: "run_hist",
        agentId: "reviewer",
        content: "历史详细正文",
        started: true,
        completed: true,
        lastEventType: "run.external.message.end",
        lastSequence: 8,
        updatedAt: "2026-04-12T14:00:00Z",
      },
    ],
    externalTasks: [
      {
        externalTaskId: "ext_hist",
        sessionId: "sess_001",
        turnId: "turn_hist",
        runId: "run_hist",
        adapter: "codex",
        remoteAgentName: "codex-reviewer",
        remoteSessionId: "remote_hist",
        remoteTaskId: "task_hist",
        state: "completed",
        lastHeartbeatAt: "2026-04-12T14:00:00Z",
        resultSummary: "历史任务已完成",
        metadata: {},
      },
    ],
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

  it("adds a new active run when RUN_STARTED arrives without waiting for a new snapshot", () => {
    const next = reduceSessionMessage(
      hydrateFromSnapshot(emptySessionViewState, makeSnapshot()),
      {
        type: "RUN_STARTED",
        runId: "run_003",
        threadId: "sess_001",
      },
    );

    expect(next.snapshot?.currentRunId).toBe("run_003");
    expect(next.snapshot?.runs.map((run) => run.runId)).toEqual(["run_002", "run_003"]);
    expect(next.snapshot?.turns[0]?.activeRunIds).toEqual(["run_002", "run_003"]);
    expect(next.snapshot?.turns[0]?.activeRunCount).toBe(2);
  });

  it("tracks external activity and links the current run to external work from incrementals", () => {
    const state = hydrateFromSnapshot(emptySessionViewState, makeSnapshot());
    const withCustom = reduceSessionMessage(state, {
      type: "CUSTOM",
      name: "axon.run.external.activity",
      value: {
        threadId: "sess_001",
        turnId: "turn_002",
        runId: "run_002",
        agentId: "default",
        externalTaskId: "ext_002",
        type: "run.external.activity",
        payload: {
          externalTaskId: "ext_002",
          kind: "delegate",
          title: "delegate to codex",
        },
      },
    });
    const next = reduceSessionMessage(withCustom, {
      type: "ACTIVITY_SNAPSHOT",
      messageId: "run_002:external",
      activityType: "run_status",
      content: {
        threadId: "sess_001",
        turnId: "turn_002",
        runId: "run_002",
        agentId: "default",
        status: "external_running",
        externalTaskId: "ext_002",
      },
      replace: true,
    });

    expect(next.snapshot?.runs[0]?.displayState).toBe("external_running");
    expect(next.snapshot?.runs[0]?.externalTaskIds).toEqual(["ext_002"]);
    expect(next.snapshot?.turns[0]?.hasExternalWork).toBe(true);
    expect(next.snapshot?.externalActivities[0]?.externalTaskId).toBe("ext_002");
  });

  it("marks the current run as failed when RUN_ERROR arrives", () => {
    const next = reduceSessionMessage(
      hydrateFromSnapshot(emptySessionViewState, makeSnapshot()),
      {
        type: "RUN_ERROR",
        runId: "run_002",
        message: "permission denied",
      },
    );

    expect(next.snapshot?.runs[0]?.displayState).toBe("failed");
    expect(next.snapshot?.runs[0]?.latestMessageText).toBe("permission denied");
    expect(next.snapshot?.turns[0]?.displayState).toBe("failed");
    expect(next.snapshot?.turns[0]?.failedRunIds).toEqual(["run_002"]);
  });

  it("merges HTTP session detail into the workbench before a websocket snapshot arrives", () => {
    const next = mergeSessionDetail(
      {
        ...emptySessionViewState,
        currentSessionId: "sess_001",
        recentSessions: [
          makeSessionItem({
            title: "stale title",
            status: "OPEN",
            preview: "stale preview",
          }),
        ],
      },
      makeSessionDetail({
        externalActivities: [],
        externalMessages: [],
      } as Partial<SessionDetail>),
      "2026-04-12T12:32:00Z",
    );

    expect(next.recentSessions[0]).toEqual({
      sessionId: "sess_001",
      title: "bootstrap title",
      status: "WAITING",
      updatedAt: "2026-04-12T12:32:00Z",
      preview: "stale preview",
    });
    expect(next.snapshot?.title).toBe("bootstrap title");
    expect(next.snapshot?.externalTasks[0]?.remoteAgentName).toBe("bootstrap-codex");
    expect(next.snapshot?.lastTurnId).toBe("turn_900");
    expect(next.snapshot?.turns[0]).toEqual({
      turnId: "turn_900",
      sessionId: "sess_001",
      primaryRunId: "run_bootstrap",
      currentRunId: "run_bootstrap",
      latestRunId: "run_bootstrap",
      latestAgentId: "bootstrap-codex",
      displayState: "waiting",
      latestEventType: "session.detail.bootstrap",
      latestMessagePreview: "Waiting for websocket snapshot...",
      eventCount: 0,
      runCount: 1,
      activeRunCount: 1,
      completedRunCount: 0,
      failedRunCount: 0,
      activeRunIds: ["run_bootstrap"],
      completedRunIds: [],
      failedRunIds: [],
      hasExternalWork: true,
      updatedAt: "2026-04-12T12:32:00Z",
    });
    expect(next.snapshot?.runs[0]).toEqual({
      runId: "run_bootstrap",
      turnId: "turn_900",
      sessionId: "sess_001",
      agentId: "bootstrap-codex",
      displayState: "external_running",
      latestEventType: "session.detail.bootstrap",
      latestMessageId: "",
      latestMessageText: "Waiting for websocket snapshot...",
      summary: "",
      externalTaskIds: ["ext_bootstrap"],
      eventCount: 0,
      updatedAt: "2026-04-12T12:32:00Z",
    });
  });

  it("prefers aggregated turns and runs from HTTP session detail when they are available", () => {
    const next = mergeSessionDetail(
      emptySessionViewState,
      makeSessionDetail({
        status: "RUNNING",
        turns: [
          {
            turnId: "turn_live",
            sessionId: "sess_001",
            primaryRunId: "run_live",
            currentRunId: "run_live",
            latestRunId: "run_live",
            latestAgentId: "default",
            displayState: "completed",
            latestEventType: "run.finished",
            latestMessagePreview: "detail summary",
            eventCount: 4,
            runCount: 1,
            activeRunCount: 0,
            completedRunCount: 1,
            failedRunCount: 0,
            activeRunIds: [],
            completedRunIds: ["run_live"],
            failedRunIds: [],
            hasExternalWork: false,
            updatedAt: "2026-04-12T13:00:00Z",
          },
        ],
        runs: [
          {
            runId: "run_live",
            turnId: "turn_live",
            sessionId: "sess_001",
            agentId: "default",
            displayState: "completed",
            latestEventType: "run.finished",
            latestMessageId: "run_live:assistant",
            latestMessageText: "detail summary",
            summary: "detail summary",
            externalTaskIds: [],
            eventCount: 4,
            updatedAt: "2026-04-12T13:00:00Z",
          },
        ],
        externalTasks: [],
      }),
      "2026-04-12T13:01:00Z",
    );

    expect(next.snapshot?.turnId).toBe("turn_live");
    expect(next.snapshot?.currentRunId).toBe("run_live");
    expect(next.snapshot?.turns[0]?.displayState).toBe("completed");
    expect(next.snapshot?.turns[0]?.latestMessagePreview).toBe("detail summary");
    expect(next.snapshot?.runs[0]?.summary).toBe("detail summary");
    expect(next.snapshot?.runs[0]?.latestMessageText).toBe("detail summary");
  });

  it("hydrates external messages and activities from HTTP session detail when available", () => {
    const next = mergeSessionDetail(
      emptySessionViewState,
      Object.assign(makeSessionDetail({
        turns: [
          {
            turnId: "turn_live",
            sessionId: "sess_001",
            primaryRunId: "run_live",
            currentRunId: "run_live",
            latestRunId: "run_live",
            latestAgentId: "default",
            displayState: "external_running",
            latestEventType: "run.external.message.end",
            latestMessagePreview: "thinking...",
            eventCount: 4,
            runCount: 1,
            activeRunCount: 1,
            completedRunCount: 0,
            failedRunCount: 0,
            activeRunIds: ["run_live"],
            completedRunIds: [],
            failedRunIds: [],
            hasExternalWork: true,
            updatedAt: "2026-04-12T13:10:00Z",
          },
        ],
        runs: [
          {
            runId: "run_live",
            turnId: "turn_live",
            sessionId: "sess_001",
            agentId: "default",
            displayState: "external_running",
            latestEventType: "run.external.message.end",
            latestMessageId: "msg_live",
            latestMessageText: "thinking...",
            summary: "",
            externalTaskIds: ["ext_live"],
            eventCount: 4,
            updatedAt: "2026-04-12T13:10:00Z",
          },
        ],
      }), {
        externalActivities: [
          {
            externalTaskId: "ext_live",
            sessionId: "sess_001",
            turnId: "turn_live",
            runId: "run_live",
            agentId: "default",
            activityId: "run_live:external",
            kind: "run_status",
            status: "external_running",
            title: "run_status",
            detail: "external_running",
            startedAt: "2026-04-12T13:09:00Z",
            updatedAt: "2026-04-12T13:10:00Z",
          },
        ],
        externalMessages: [
          {
            externalTaskId: "ext_live",
            messageId: "msg_live",
            sessionId: "sess_001",
            turnId: "turn_live",
            runId: "run_live",
            agentId: "default",
            content: "thinking...",
            started: true,
            completed: true,
            lastEventType: "run.external.message.end",
            lastSequence: 5,
            updatedAt: "2026-04-12T13:10:00Z",
          },
        ],
      }),
      "2026-04-12T13:11:00Z",
    );

    expect((next.snapshot as StateSnapshotV0)?.externalActivities[0]?.activityId).toBe("run_live:external");
    expect((next.snapshot as StateSnapshotV0)?.externalMessages[0]?.messageId).toBe("msg_live");
    expect((next.snapshot as StateSnapshotV0)?.externalMessages[0]?.content).toBe("thinking...");
  });

  it("merges on-demand run detail into the current snapshot and focuses the restored run", () => {
    const next = mergeRunDetail(
      hydrateFromSnapshot(
        {
          ...emptySessionViewState,
          focusedRunId: "run_hist",
        },
        makeSnapshot({
          turnId: "turn_current",
          lastTurnId: "turn_current",
          currentRunId: "run_current",
          turns: [
            {
              turnId: "turn_current",
              sessionId: "sess_001",
              primaryRunId: "run_current",
              currentRunId: "run_current",
              latestRunId: "run_current",
              latestAgentId: "default",
              displayState: "running",
              latestEventType: "run.started",
              latestMessagePreview: "当前运行中",
              eventCount: 2,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_current"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: false,
              updatedAt: "2026-04-12T13:50:00Z",
            },
          ],
          runs: [
            {
              runId: "run_current",
              turnId: "turn_current",
              sessionId: "sess_001",
              agentId: "default",
              displayState: "running",
              latestEventType: "run.started",
              latestMessageId: "msg_current",
              latestMessageText: "当前运行中",
              summary: "",
              externalTaskIds: [],
              eventCount: 2,
              updatedAt: "2026-04-12T13:50:00Z",
            },
            {
              runId: "run_hist",
              turnId: "turn_hist",
              sessionId: "sess_001",
              agentId: "reviewer",
              displayState: "completed",
              latestEventType: "run.finished",
              latestMessageId: "",
              latestMessageText: "",
              summary: "",
              externalTaskIds: [],
              eventCount: 0,
              updatedAt: "2026-04-12T13:40:00Z",
            },
          ],
        }),
      ),
      makeRunDetail(),
      "2026-04-12T14:01:00Z",
    );

    expect(next.focusedRunId).toBe("run_hist");
    expect(next.snapshot?.runs.map((run) => run.runId)).toEqual(["run_current", "run_hist"]);
    expect(next.snapshot?.runs.find((run) => run.runId === "run_hist")?.summary).toBe("历史 run 摘要");
    expect(next.snapshot?.turns.find((turn) => turn.turnId === "turn_hist")?.latestMessagePreview).toBe("历史 run 摘要");
    expect(next.snapshot?.externalMessages.find((message) => message.runId === "run_hist")?.content).toBe("历史详细正文");
    expect(next.snapshot?.externalTasks.find((task) => task.runId === "run_hist")?.remoteAgentName).toBe("codex-reviewer");
  });
});
