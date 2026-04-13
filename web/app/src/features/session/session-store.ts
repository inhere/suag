import type {
  ActivitySnapshotEvent,
  CustomEvent,
  HostEvent,
  RunDetail,
  RunCanceledEvent,
  RunErrorEvent,
  RunStartedEvent,
  SessionDetail,
  SessionSocketMessage,
  SessionStatus,
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
  StateSnapshotV0,
} from "../../lib/host-types";
import { selectFocusedRun, selectSessionPreview } from "./session-selectors";

export type SessionFilter = "all" | "running" | "waiting";

export type SessionListItem = {
  sessionId: string;
  title: string;
  status: SessionStatus | string;
  updatedAt: string;
  preview: string;
};

export type SessionViewState = {
  recentSessions: SessionListItem[];
  currentSessionId: string | null;
  snapshot: StateSnapshotV0 | null;
  focusedRunId: string | null;
  filter: SessionFilter;
  runDetailStatus: Record<string, "idle" | "loading" | "loaded" | "error">;
};

export const emptySessionViewState: SessionViewState = {
  recentSessions: [],
  currentSessionId: null,
  snapshot: null,
  focusedRunId: null,
  filter: "all",
  runDetailStatus: {},
};

function upsertRecentSession(
  sessions: SessionListItem[],
  next: SessionListItem,
): SessionListItem[] {
  const filtered = sessions.filter((item) => item.sessionId !== next.sessionId);
  return [next, ...filtered].sort((left, right) =>
    right.updatedAt.localeCompare(left.updatedAt),
  );
}

function appendSnapshotEvent(
  snapshot: StateSnapshotV0,
  event: SessionSocketMessage,
): StateSnapshotV0 {
  const normalizedEvent: HostEvent = "snapshot" in event
    ? {
        type: event.type,
        snapshot: event.snapshot,
      }
    : ("value" in event || "sequence" in event || "delta" in event || "messageId" in event)
      ? (event as HostEvent)
      : {
          type: event.type,
        };

  return {
    ...snapshot,
    events: [...snapshot.events, normalizedEvent],
  };
}

function createBootstrapSnapshot(
  detail: SessionDetail,
  generatedAt: string,
): StateSnapshotV0 {
  const bootstrapTask = detail.externalTasks[0] ?? null;
  const bootstrapRunId = bootstrapTask?.runId ?? "";
  const bootstrapAgentID = bootstrapTask?.remoteAgentName ?? "";
  const bootstrapTurnID = detail.lastTurnId;

  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt,
    threadId: detail.sessionId,
    sessionId: detail.sessionId,
    title: detail.title,
    status: detail.status,
    turnId: bootstrapTurnID,
    lastTurnId: bootstrapTurnID,
    currentRunId: bootstrapRunId,
    currentAgentId: bootstrapAgentID,
    latestEventType: "",
    eventCount: 0,
    // Build a minimal turn/run scaffold from HTTP detail so the workbench
    // does not stay blank before the websocket sends the first snapshot.
    turns: bootstrapTurnID
      ? [
          {
            turnId: bootstrapTurnID,
            sessionId: detail.sessionId,
            primaryRunId: bootstrapRunId,
            currentRunId: bootstrapRunId,
            latestRunId: bootstrapRunId,
            latestAgentId: bootstrapAgentID,
            displayState: detail.status.toLowerCase(),
            latestEventType: "session.detail.bootstrap",
            latestMessagePreview: "Waiting for websocket snapshot...",
            eventCount: 0,
            runCount: bootstrapRunId ? 1 : 0,
            activeRunCount: bootstrapRunId ? 1 : 0,
            completedRunCount: 0,
            failedRunCount: 0,
            activeRunIds: bootstrapRunId ? [bootstrapRunId] : [],
            completedRunIds: [],
            failedRunIds: [],
            hasExternalWork: detail.externalTasks.length > 0,
            updatedAt: generatedAt,
          },
        ]
      : [],
    runs: bootstrapRunId
      ? [
          {
            runId: bootstrapRunId,
            turnId: bootstrapTurnID,
            sessionId: detail.sessionId,
            agentId: bootstrapAgentID,
            displayState: detail.externalTasks.length ? "external_running" : detail.status.toLowerCase(),
            latestEventType: "session.detail.bootstrap",
            latestMessageId: "",
            latestMessageText: "Waiting for websocket snapshot...",
            summary: "",
            externalTaskIds: bootstrapTask ? [bootstrapTask.externalTaskId] : [],
            eventCount: 0,
            updatedAt: generatedAt,
          },
        ]
      : [],
    externalTasks: detail.externalTasks,
    externalActivities: [],
    externalMessages: [],
    events: [],
  };
}

function createDetailSnapshot(
  detail: SessionDetail,
  generatedAt: string,
): StateSnapshotV0 | null {
  if (
    !detail.turns.length &&
    !detail.runs.length &&
    !detail.externalActivities.length &&
    !detail.externalMessages.length
  ) {
    return null;
  }

  const currentTurn = detail.turns.find((turn) => turn.turnId === detail.lastTurnId) ?? detail.turns[0] ?? null;
  const currentRun =
    detail.runs.find((run) => run.runId === currentTurn?.currentRunId) ??
    detail.runs.find((run) => run.turnId === currentTurn?.turnId) ??
    detail.runs[0] ??
    null;

  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt,
    threadId: detail.sessionId,
    sessionId: detail.sessionId,
    title: detail.title,
    status: detail.status,
    turnId: currentTurn?.turnId ?? detail.lastTurnId,
    lastTurnId: detail.lastTurnId,
    currentRunId: currentRun?.runId ?? "",
    currentAgentId: currentRun?.agentId ?? currentTurn?.latestAgentId ?? "",
    latestEventType: currentRun?.latestEventType ?? currentTurn?.latestEventType ?? "",
    eventCount: 0,
    turns: detail.turns,
    runs: detail.runs,
    externalTasks: detail.externalTasks,
    externalActivities: detail.externalActivities,
    externalMessages: detail.externalMessages,
    events: [],
  };
}

function isStateSnapshotMessage(
  message: SessionSocketMessage,
): message is Extract<SessionSocketMessage, { type: "STATE_SNAPSHOT" }> {
  return message.type === "STATE_SNAPSHOT" && "snapshot" in message && !!message.snapshot;
}

function isTextMessageStart(
  message: SessionSocketMessage,
): message is Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_START" }> {
  return message.type === "TEXT_MESSAGE_START" && "messageId" in message;
}

function isTextMessageContent(
  message: SessionSocketMessage,
): message is Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_CONTENT" }> {
  return message.type === "TEXT_MESSAGE_CONTENT" && "messageId" in message && "delta" in message;
}

function isTextMessageEnd(
  message: SessionSocketMessage,
): message is Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_END" }> {
  return message.type === "TEXT_MESSAGE_END" && "messageId" in message;
}

function isRunFinishedMessage(
  message: SessionSocketMessage,
): message is Extract<SessionSocketMessage, { type: "RUN_FINISHED" }> {
  return message.type === "RUN_FINISHED" && "runId" in message;
}

function isRunStartedMessage(
  message: SessionSocketMessage,
): message is RunStartedEvent {
  return message.type === "RUN_STARTED" && "runId" in message && "threadId" in message;
}

function isRunErrorMessage(
  message: SessionSocketMessage,
): message is RunErrorEvent {
  return message.type === "RUN_ERROR" && "runId" in message && "message" in message;
}

function isRunCanceledMessage(
  message: SessionSocketMessage,
): message is RunCanceledEvent {
  return message.type === "RUN_CANCELED" && "runId" in message && "message" in message;
}

function isActivitySnapshotMessage(
  message: SessionSocketMessage,
): message is ActivitySnapshotEvent {
  return message.type === "ACTIVITY_SNAPSHOT" && "messageId" in message && "content" in message;
}

function isCustomMessage(
  message: SessionSocketMessage,
): message is CustomEvent {
  return message.type === "CUSTOM" && "name" in message;
}

function cloneRun(run: StateSnapshotRun): StateSnapshotRun {
  return {
    ...run,
    externalTaskIds: [...run.externalTaskIds],
  };
}

function cloneTurn(turn: StateSnapshotTurn): StateSnapshotTurn {
  return {
    ...turn,
    activeRunIds: [...turn.activeRunIds],
    completedRunIds: [...turn.completedRunIds],
    failedRunIds: [...turn.failedRunIds],
  };
}

function cloneExternalMessage(
  message: StateSnapshotExternalMessage,
): StateSnapshotExternalMessage {
  return { ...message };
}

function cloneExternalActivity(
  activity: StateSnapshotExternalActivity,
): StateSnapshotExternalActivity {
  return { ...activity };
}

function upsertRun(
  runs: StateSnapshotRun[],
  nextRun: StateSnapshotRun,
): StateSnapshotRun[] {
  const existingIndex = runs.findIndex((run) => run.runId === nextRun.runId);
  if (existingIndex < 0) {
    return [...runs, nextRun];
  }

  const mergedRuns = [...runs];
  mergedRuns[existingIndex] = nextRun;
  return mergedRuns;
}

function upsertTurn(
  turns: StateSnapshotTurn[],
  nextTurn: StateSnapshotTurn,
): StateSnapshotTurn[] {
  const existingIndex = turns.findIndex((turn) => turn.turnId === nextTurn.turnId);
  if (existingIndex < 0) {
    return [...turns, nextTurn];
  }

  const mergedTurns = [...turns];
  mergedTurns[existingIndex] = nextTurn;
  return mergedTurns;
}

function replaceItemsByRunID<T extends { runId: string }>(
  items: T[],
  runId: string,
  nextItems: T[],
): T[] {
  return [...items.filter((item) => item.runId !== runId), ...nextItems];
}

function hasRunDetailState(
  snapshot: Pick<StateSnapshotV0, "externalActivities" | "externalMessages">,
  runID: string,
): boolean {
  const externalActivities = snapshot.externalActivities ?? [];
  const externalMessages = snapshot.externalMessages ?? [];
  return (
    externalActivities.some((activity) => activity.runId === runID) ||
    externalMessages.some((message) => message.runId === runID)
  );
}

function getTargetRun(snapshot: StateSnapshotV0): StateSnapshotRun | null {
  if (!snapshot.runs.length) {
    return null;
  }

  return snapshot.runs.find((run) => run.runId === snapshot.currentRunId) ?? snapshot.runs[0] ?? null;
}

function getTargetTurn(snapshot: StateSnapshotV0): StateSnapshotTurn | null {
  if (!snapshot.turns.length) {
    return null;
  }

  return snapshot.turns.find((turn) => turn.turnId === snapshot.turnId) ?? snapshot.turns[0] ?? null;
}

function ensureRunExists(
  runs: StateSnapshotRun[],
  turn: StateSnapshotTurn,
  snapshot: StateSnapshotV0,
  runId: string,
  agentId: string,
): StateSnapshotRun {
  const existing = runs.find((run) => run.runId === runId);
  if (existing) {
    return existing;
  }

  const nextRun: StateSnapshotRun = {
    runId,
    turnId: turn.turnId,
    sessionId: snapshot.sessionId,
    agentId,
    displayState: "running",
    latestEventType: "run.started",
    latestMessageId: "",
    latestMessageText: "",
    summary: "",
    externalTaskIds: [],
    eventCount: 0,
    updatedAt: snapshot.generatedAt,
  };
  runs.push(nextRun);
  turn.runCount += 1;
  return nextRun;
}

function markRunActive(turn: StateSnapshotTurn, runId: string) {
  if (!turn.activeRunIds.includes(runId)) {
    turn.activeRunIds.push(runId);
  }
  turn.completedRunIds = turn.completedRunIds.filter((id) => id !== runId);
  turn.failedRunIds = turn.failedRunIds.filter((id) => id !== runId);
  turn.activeRunCount = turn.activeRunIds.length;
  turn.completedRunCount = turn.completedRunIds.length;
  turn.failedRunCount = turn.failedRunIds.length;
}

function markRunFailed(turn: StateSnapshotTurn, runId: string) {
  turn.activeRunIds = turn.activeRunIds.filter((id) => id !== runId);
  if (!turn.failedRunIds.includes(runId)) {
    turn.failedRunIds.push(runId);
  }
  turn.activeRunCount = turn.activeRunIds.length;
  turn.failedRunCount = turn.failedRunIds.length;
}

function addExternalTaskID(run: StateSnapshotRun, externalTaskId: string) {
  if (!externalTaskId) {
    return;
  }

  if (!run.externalTaskIds.includes(externalTaskId)) {
    run.externalTaskIds.push(externalTaskId);
  }
}

function reduceTextMessageStart(
  snapshot: StateSnapshotV0,
  message: Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_START" }>,
): StateSnapshotV0 {
  if (snapshot.externalMessages.some((item) => item.messageId === message.messageId)) {
    return appendSnapshotEvent(snapshot, message);
  }

  const targetRun = getTargetRun(snapshot);
  const targetTurn = getTargetTurn(snapshot);
  if (!targetRun || !targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  return appendSnapshotEvent({
    ...snapshot,
    externalMessages: [
      ...snapshot.externalMessages,
      {
        externalTaskId: targetRun.externalTaskIds[0] ?? "",
        messageId: message.messageId,
        sessionId: snapshot.sessionId,
        turnId: targetTurn.turnId,
        runId: targetRun.runId,
        agentId: targetRun.agentId,
        content: "",
        started: true,
        completed: false,
        lastEventType: message.type,
        lastSequence: message.timestamp ?? 0,
        updatedAt: snapshot.generatedAt,
      },
    ],
  }, message);
}

function reduceTextMessageContent(
  snapshot: StateSnapshotV0,
  message: Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_CONTENT" }>,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const externalMessages = snapshot.externalMessages.map(cloneExternalMessage);
  const targetRun = getTargetRun({ ...snapshot, runs, turns, externalMessages });
  const targetTurn = getTargetTurn({ ...snapshot, runs, turns, externalMessages });

  if (!targetRun || !targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  const messageIndex = externalMessages.findIndex((item) => item.messageId === message.messageId);
  const nextExternalMessage =
    messageIndex >= 0
      ? externalMessages[messageIndex]
      : {
          externalTaskId: targetRun.externalTaskIds[0] ?? "",
          messageId: message.messageId,
          sessionId: snapshot.sessionId,
          turnId: targetTurn.turnId,
          runId: targetRun.runId,
          agentId: targetRun.agentId,
          content: targetRun.latestMessageText,
          started: true,
          completed: false,
          lastEventType: message.type,
          lastSequence: message.timestamp ?? 0,
          updatedAt: snapshot.generatedAt,
        };

  nextExternalMessage.content += message.delta;
  nextExternalMessage.lastEventType = message.type;
  nextExternalMessage.lastSequence = message.timestamp ?? nextExternalMessage.lastSequence;
  nextExternalMessage.updatedAt = snapshot.generatedAt;

  if (messageIndex < 0) {
    externalMessages.push(nextExternalMessage);
  }

  if (!targetRun.summary) {
    targetRun.latestMessageId = message.messageId;
    targetRun.latestMessageText += message.delta;
  }

  targetRun.latestEventType = message.type;
  targetTurn.latestMessagePreview = targetRun.summary || targetRun.latestMessageText;
  targetTurn.latestEventType = message.type;

  return appendSnapshotEvent(
    {
      ...snapshot,
      runs,
      turns,
      externalMessages,
    },
    message,
  );
}

function reduceTextMessageEnd(
  snapshot: StateSnapshotV0,
  message: Extract<SessionSocketMessage, { type: "TEXT_MESSAGE_END" }>,
): StateSnapshotV0 {
  const externalMessages = snapshot.externalMessages.map(cloneExternalMessage);
  const target = externalMessages.find((item) => item.messageId === message.messageId);
  if (!target) {
    return appendSnapshotEvent(snapshot, message);
  }

  target.completed = true;
  target.lastEventType = message.type;
  target.lastSequence = message.timestamp ?? target.lastSequence;
  target.updatedAt = snapshot.generatedAt;

  return appendSnapshotEvent(
    {
      ...snapshot,
      externalMessages,
    },
    message,
  );
}

function reduceRunStarted(
  snapshot: StateSnapshotV0,
  message: RunStartedEvent,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const targetTurn = getTargetTurn({ ...snapshot, turns });
  if (!targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  const nextRun = ensureRunExists(
    runs,
    targetTurn,
    snapshot,
    message.runId,
    snapshot.currentAgentId || "default",
  );

  nextRun.displayState = "running";
  nextRun.latestEventType = "run.started";
  nextRun.updatedAt = snapshot.generatedAt;
  nextRun.eventCount += 1;

  targetTurn.currentRunId = nextRun.runId;
  targetTurn.latestRunId = nextRun.runId;
  targetTurn.displayState = "running";
  targetTurn.latestEventType = "run.started";
  targetTurn.updatedAt = snapshot.generatedAt;
  targetTurn.eventCount += 1;
  markRunActive(targetTurn, nextRun.runId);

  return appendSnapshotEvent(
    {
      ...snapshot,
      currentRunId: nextRun.runId,
      currentAgentId: nextRun.agentId,
      latestEventType: "run.started",
      runs,
      turns,
    },
    message,
  );
}

function reduceRunFinished(
  snapshot: StateSnapshotV0,
  message: Extract<SessionSocketMessage, { type: "RUN_FINISHED" }>,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const targetRun = runs.find((run) => run.runId === message.runId) ?? getTargetRun({ ...snapshot, runs, turns });
  const targetTurn = getTargetTurn({ ...snapshot, runs, turns });
  const result =
    "result" in message && typeof message.result === "object" && message.result
      ? (message.result as Record<string, unknown>)
      : {};
  const summary = typeof result.summary === "string" ? result.summary : "";

  if (!targetRun || !targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  targetRun.displayState = "completed";
  targetRun.latestEventType = "run.finished";
  if (summary) {
    targetRun.summary = summary;
    targetRun.latestMessageText = summary;
    targetRun.latestMessageId = `${targetRun.runId}:assistant`;
    targetTurn.latestMessagePreview = summary;
  }

  targetTurn.displayState = "completed";
  targetTurn.latestEventType = "run.finished";
  targetTurn.currentRunId = targetRun.runId;
  targetTurn.latestRunId = targetRun.runId;

  return appendSnapshotEvent(
    {
      ...snapshot,
      latestEventType: "run.finished",
      runs,
      turns,
    },
    message,
  );
}

function reduceRunError(
  snapshot: StateSnapshotV0,
  message: RunErrorEvent,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const targetRun =
    runs.find((run) => run.runId === message.runId) ?? getTargetRun({ ...snapshot, runs, turns });
  const targetTurn = getTargetTurn({ ...snapshot, runs, turns });
  if (!targetRun || !targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  targetRun.displayState = "failed";
  targetRun.latestEventType = "run.error";
  targetRun.latestMessageText = message.message;
  targetRun.updatedAt = snapshot.generatedAt;
  targetRun.eventCount += 1;

  targetTurn.displayState = "failed";
  targetTurn.latestEventType = "run.error";
  targetTurn.latestMessagePreview = message.message;
  targetTurn.updatedAt = snapshot.generatedAt;
  targetTurn.eventCount += 1;
  markRunFailed(targetTurn, targetRun.runId);

  return appendSnapshotEvent(
    {
      ...snapshot,
      latestEventType: "run.error",
      runs,
      turns,
    },
    message,
  );
}

function reduceRunCanceled(
  snapshot: StateSnapshotV0,
  message: RunCanceledEvent,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const targetRun =
    runs.find((run) => run.runId === message.runId) ?? getTargetRun({ ...snapshot, runs, turns });
  const targetTurn = getTargetTurn({ ...snapshot, runs, turns });
  if (!targetRun || !targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  targetRun.displayState = "canceled";
  targetRun.latestEventType = "run.canceled";
  targetRun.latestMessageText = message.message;
  targetRun.updatedAt = snapshot.generatedAt;
  targetRun.eventCount += 1;

  targetTurn.displayState = "canceled";
  targetTurn.latestEventType = "run.canceled";
  targetTurn.latestMessagePreview = message.message;
  targetTurn.updatedAt = snapshot.generatedAt;
  targetTurn.eventCount += 1;
  targetTurn.activeRunIds = targetTurn.activeRunIds.filter((id) => id !== targetRun.runId);
  targetTurn.activeRunCount = targetTurn.activeRunIds.length;
  targetTurn.currentRunId = targetRun.runId;
  targetTurn.latestRunId = targetRun.runId;

  return appendSnapshotEvent(
    {
      ...snapshot,
      latestEventType: "run.canceled",
      runs,
      turns,
    },
    message,
  );
}

function reduceCustomExternalActivity(
  snapshot: StateSnapshotV0,
  message: CustomEvent,
): StateSnapshotV0 {
  if (message.name !== "axon.run.external.activity" || !message.value) {
    return appendSnapshotEvent(snapshot, message);
  }

  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const externalActivities = snapshot.externalActivities.map(cloneExternalActivity);
  const runId = typeof message.value.runId === "string" ? message.value.runId : "";
  const turnId = typeof message.value.turnId === "string" ? message.value.turnId : snapshot.turnId;
  const agentId =
    typeof message.value.agentId === "string" ? message.value.agentId : snapshot.currentAgentId;
  const externalTaskId =
    typeof message.value.externalTaskId === "string" ? message.value.externalTaskId : "";

  const targetTurn =
    turns.find((turn) => turn.turnId === turnId) ?? getTargetTurn({ ...snapshot, turns });
  if (!targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  const targetRun = ensureRunExists(
    runs,
    targetTurn,
    snapshot,
    runId || targetTurn.currentRunId,
    agentId || "default",
  );
  targetRun.displayState = "external_running";
  targetRun.latestEventType = "run.external.activity";
  targetRun.updatedAt = snapshot.generatedAt;
  targetRun.eventCount += 1;
  addExternalTaskID(targetRun, externalTaskId);

  targetTurn.displayState = "external_running";
  targetTurn.latestEventType = "run.external.activity";
  targetTurn.currentRunId = targetRun.runId;
  targetTurn.latestRunId = targetRun.runId;
  targetTurn.hasExternalWork = true;
  targetTurn.updatedAt = snapshot.generatedAt;
  targetTurn.eventCount += 1;
  markRunActive(targetTurn, targetRun.runId);

  const existingIndex = externalActivities.findIndex(
    (activity) => activity.runId === targetRun.runId && activity.externalTaskId === externalTaskId,
  );
  const payload =
    typeof message.value.payload === "object" && message.value.payload
      ? (message.value.payload as Record<string, unknown>)
      : {};
  const nextActivity: StateSnapshotExternalActivity = {
    externalTaskId,
    sessionId: snapshot.sessionId,
    turnId: targetTurn.turnId,
    runId: targetRun.runId,
    agentId: targetRun.agentId,
    activityId:
      typeof message.value.sequence === "number"
        ? `seq_${message.value.sequence}`
        : `${targetRun.runId}:external`,
    kind: typeof payload.kind === "string" ? payload.kind : "external_activity",
    status: "running",
    title: typeof payload.title === "string" ? payload.title : "External activity",
    detail:
      typeof message.value.type === "string" ? message.value.type : "run.external.activity",
    startedAt: snapshot.generatedAt,
    updatedAt: snapshot.generatedAt,
  };

  if (existingIndex >= 0) {
    externalActivities[existingIndex] = {
      ...externalActivities[existingIndex],
      ...nextActivity,
    };
  } else {
    externalActivities.push(nextActivity);
  }

  return appendSnapshotEvent(
    {
      ...snapshot,
      currentRunId: targetRun.runId,
      latestEventType: "run.external.activity",
      runs,
      turns,
      externalActivities,
    },
    message,
  );
}

function reduceActivitySnapshot(
  snapshot: StateSnapshotV0,
  message: ActivitySnapshotEvent,
): StateSnapshotV0 {
  const runs = snapshot.runs.map(cloneRun);
  const turns = snapshot.turns.map(cloneTurn);
  const externalActivities = snapshot.externalActivities.map(cloneExternalActivity);
  const runId = typeof message.content.runId === "string" ? message.content.runId : "";
  const turnId = typeof message.content.turnId === "string" ? message.content.turnId : snapshot.turnId;
  const agentId =
    typeof message.content.agentId === "string" ? message.content.agentId : snapshot.currentAgentId;
  const externalTaskId =
    typeof message.content.externalTaskId === "string" ? message.content.externalTaskId : "";
  const status = typeof message.content.status === "string" ? message.content.status : "running";

  const targetTurn =
    turns.find((turn) => turn.turnId === turnId) ?? getTargetTurn({ ...snapshot, turns });
  if (!targetTurn) {
    return appendSnapshotEvent(snapshot, message);
  }

  const targetRun = ensureRunExists(
    runs,
    targetTurn,
    snapshot,
    runId || targetTurn.currentRunId,
    agentId || "default",
  );
  targetRun.displayState = status;
  targetRun.latestEventType = "activity.snapshot";
  targetRun.updatedAt = snapshot.generatedAt;
  targetRun.eventCount += 1;
  addExternalTaskID(targetRun, externalTaskId);

  targetTurn.displayState = status;
  targetTurn.latestEventType = "activity.snapshot";
  targetTurn.currentRunId = targetRun.runId;
  targetTurn.latestRunId = targetRun.runId;
  targetTurn.updatedAt = snapshot.generatedAt;
  targetTurn.eventCount += 1;
  if (status === "external_running") {
    targetTurn.hasExternalWork = true;
  }
  markRunActive(targetTurn, targetRun.runId);

  const existingIndex = externalActivities.findIndex(
    (activity) => activity.activityId === message.messageId,
  );
  const nextActivity: StateSnapshotExternalActivity = {
    externalTaskId,
    sessionId: snapshot.sessionId,
    turnId: targetTurn.turnId,
    runId: targetRun.runId,
    agentId: targetRun.agentId,
    activityId: message.messageId,
    kind: message.activityType,
    status,
    title: message.activityType,
    detail: status,
    startedAt: snapshot.generatedAt,
    updatedAt: snapshot.generatedAt,
  };

  if (existingIndex >= 0) {
    externalActivities[existingIndex] = {
      ...externalActivities[existingIndex],
      ...nextActivity,
    };
  } else {
    externalActivities.push(nextActivity);
  }

  return appendSnapshotEvent(
    {
      ...snapshot,
      currentRunId: targetRun.runId,
      latestEventType: "activity.snapshot",
      runs,
      turns,
      externalActivities,
    },
    message,
  );
}

function reduceIncrementalMessage(
  snapshot: StateSnapshotV0,
  message: SessionSocketMessage,
): StateSnapshotV0 {
  if (isRunStartedMessage(message)) {
    return reduceRunStarted(snapshot, message);
  }

  if (isTextMessageStart(message)) {
    return reduceTextMessageStart(snapshot, message);
  }

  if (isTextMessageContent(message)) {
    return reduceTextMessageContent(snapshot, message);
  }

  if (isTextMessageEnd(message)) {
    return reduceTextMessageEnd(snapshot, message);
  }

  if (isRunFinishedMessage(message)) {
    return reduceRunFinished(snapshot, message);
  }

  if (isRunErrorMessage(message)) {
    return reduceRunError(snapshot, message);
  }

  if (isRunCanceledMessage(message)) {
    return reduceRunCanceled(snapshot, message);
  }

  if (isCustomMessage(message)) {
    return reduceCustomExternalActivity(snapshot, message);
  }

  if (isActivitySnapshotMessage(message)) {
    return reduceActivitySnapshot(snapshot, message);
  }

  return appendSnapshotEvent(snapshot, message);
}

export function hydrateFromSnapshot(
  state: SessionViewState,
  snapshot: StateSnapshotV0,
): SessionViewState {
  const focusedRun = selectFocusedRun(snapshot, state.focusedRunId);

  return {
    ...state,
    currentSessionId: snapshot.sessionId,
    snapshot,
    focusedRunId: focusedRun?.runId ?? null,
    runDetailStatus: Object.fromEntries(
      Object.entries(state.runDetailStatus).filter(([runId]) =>
        snapshot.runs.some((run) => run.runId === runId),
      ),
    ),
    recentSessions: upsertRecentSession(state.recentSessions, {
      sessionId: snapshot.sessionId,
      title: snapshot.title,
      status: snapshot.status,
      updatedAt: snapshot.generatedAt,
      preview: selectSessionPreview(snapshot),
    }),
  };
}

export function mergeSessionDetail(
  state: SessionViewState,
  detail: SessionDetail,
  observedAt: string,
): SessionViewState {
  const detailSnapshot =
    createDetailSnapshot(detail, observedAt) ?? createBootstrapSnapshot(detail, observedAt);
  const detailExternalActivities = detailSnapshot.externalActivities ?? [];
  const detailExternalMessages = detailSnapshot.externalMessages ?? [];
  const nextSnapshot = state.snapshot
    ? {
        ...state.snapshot,
        sessionId: detail.sessionId,
        threadId: detail.sessionId,
        title: detail.title,
        status: detail.status,
        turnId: state.snapshot.turnId || detailSnapshot.turnId,
        lastTurnId: detail.lastTurnId,
        currentRunId: state.snapshot.currentRunId || detailSnapshot.currentRunId,
        currentAgentId:
          state.snapshot.currentAgentId || detailSnapshot.currentAgentId,
        latestEventType: state.snapshot.latestEventType || detailSnapshot.latestEventType,
        externalTasks: detail.externalTasks,
        externalActivities:
          state.snapshot.externalActivities.length > 0
            ? state.snapshot.externalActivities
            : detailExternalActivities,
        externalMessages:
          state.snapshot.externalMessages.length > 0
            ? state.snapshot.externalMessages
            : detailExternalMessages,
        generatedAt: observedAt,
        turns:
          state.snapshot.turns.length > 0
            ? state.snapshot.turns
            : detailSnapshot.turns,
        runs:
          state.snapshot.runs.length > 0
            ? state.snapshot.runs
            : detailSnapshot.runs,
      }
    : detailSnapshot;

  return {
    ...state,
    currentSessionId: detail.sessionId,
    snapshot: nextSnapshot,
    runDetailStatus: nextSnapshot.runs.reduce<Record<string, "idle" | "loading" | "loaded" | "error">>(
      (acc, run) => {
        const hasDetailedExternalState = hasRunDetailState(nextSnapshot, run.runId);
        acc[run.runId] = hasDetailedExternalState ? "loaded" : state.runDetailStatus[run.runId] ?? "idle";
        return acc;
      },
      {},
    ),
    recentSessions: upsertRecentSession(state.recentSessions, {
      sessionId: detail.sessionId,
      title: detail.title,
      status: detail.status,
      updatedAt: observedAt,
      preview:
        state.recentSessions.find((item) => item.sessionId === detail.sessionId)?.preview ?? "",
    }),
  };
}

export function mergeRunDetail(
  state: SessionViewState,
  detail: RunDetail,
  observedAt: string,
): SessionViewState {
  if (!state.snapshot || !detail.run) {
    return state;
  }

  const turns = detail.turn
    ? upsertTurn(state.snapshot.turns, detail.turn)
    : state.snapshot.turns;
  const runs = upsertRun(state.snapshot.runs, detail.run);
  const externalActivities = replaceItemsByRunID(
    state.snapshot.externalActivities,
    detail.run.runId,
    detail.externalActivities,
  );
  const externalMessages = replaceItemsByRunID(
    state.snapshot.externalMessages,
    detail.run.runId,
    detail.externalMessages,
  );
  const externalTasks = replaceItemsByRunID(
    state.snapshot.externalTasks,
    detail.run.runId,
    detail.externalTasks,
  );

  return {
    ...state,
    focusedRunId: detail.run.runId,
    runDetailStatus: {
      ...state.runDetailStatus,
      [detail.run.runId]: "loaded",
    },
    snapshot: {
      ...state.snapshot,
      generatedAt: observedAt,
      lastTurnId: detail.turnId || state.snapshot.lastTurnId,
      turns,
      runs,
      externalActivities,
      externalMessages,
      externalTasks,
    },
  };
}

export function markRunDetailRequested(
  state: SessionViewState,
  runId: string,
): SessionViewState {
  if (!runId) {
    return state;
  }

  return {
    ...state,
    runDetailStatus: {
      ...state.runDetailStatus,
      [runId]: "loading",
    },
  };
}

export function markRunDetailFailed(
  state: SessionViewState,
  runId: string,
): SessionViewState {
  if (!runId) {
    return state;
  }

  return {
    ...state,
    runDetailStatus: {
      ...state.runDetailStatus,
      [runId]: "error",
    },
  };
}

export function markRunDetailIdle(
  state: SessionViewState,
  runId: string,
): SessionViewState {
  if (!runId) {
    return state;
  }

  return {
    ...state,
    runDetailStatus: {
      ...state.runDetailStatus,
      [runId]: "idle",
    },
  };
}

export function shouldLoadRunDetail(
  state: SessionViewState,
  runId: string | null,
): boolean {
  if (!state.snapshot || !runId) {
    return false;
  }

  const status = state.runDetailStatus[runId] ?? "idle";
  if (status !== "idle") {
    return false;
  }

  const runExists = state.snapshot.runs.some((run) => run.runId === runId);
  if (!runExists) {
    return false;
  }

  const hasDetailedExternalState = hasRunDetailState(state.snapshot, runId);

  return !hasDetailedExternalState;
}

export function reduceSessionMessage(
  state: SessionViewState,
  message: SessionSocketMessage,
): SessionViewState {
  if (isStateSnapshotMessage(message)) {
    return hydrateFromSnapshot(state, message.snapshot);
  }

  if (!state.snapshot) {
    return state;
  }

  const nextSnapshot = reduceIncrementalMessage(state.snapshot, message);
  return hydrateFromSnapshot(
    {
      ...state,
      snapshot: nextSnapshot,
    },
    nextSnapshot,
  );
}
