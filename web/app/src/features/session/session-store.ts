import type {
  HostEvent,
  SessionSocketMessage,
  SessionStatus,
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
};

export const emptySessionViewState: SessionViewState = {
  recentSessions: [],
  currentSessionId: null,
  snapshot: null,
  focusedRunId: null,
  filter: "all",
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

function reduceIncrementalMessage(
  snapshot: StateSnapshotV0,
  message: SessionSocketMessage,
): StateSnapshotV0 {
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
    recentSessions: upsertRecentSession(state.recentSessions, {
      sessionId: snapshot.sessionId,
      title: snapshot.title,
      status: snapshot.status,
      updatedAt: snapshot.generatedAt,
      preview: selectSessionPreview(snapshot),
    }),
  };
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
