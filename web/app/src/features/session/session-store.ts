import type {
  SessionSocketMessage,
  SessionStatus,
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
  if (message.type === "STATE_SNAPSHOT") {
    return hydrateFromSnapshot(state, message.snapshot);
  }

  return state;
}
