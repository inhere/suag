import type { StateSnapshotRun, StateSnapshotTurn, StateSnapshotV0 } from "../../lib/host-types";

function isActiveRunState(displayState: string): boolean {
  return !["completed", "failed", "canceled"].includes(displayState.toLowerCase());
}

export function selectCurrentTurn(
  snapshot: StateSnapshotV0 | null,
): StateSnapshotTurn | null {
  if (!snapshot) {
    return null;
  }

  return (
    snapshot.turns.find((turn) => turn.turnId === snapshot.turnId) ??
    snapshot.turns.find((turn) => turn.turnId === snapshot.lastTurnId) ??
    snapshot.turns[0] ??
    null
  );
}

export function selectActiveRuns(snapshot: StateSnapshotV0 | null): StateSnapshotRun[] {
  if (!snapshot) {
    return [];
  }

  return snapshot.runs.filter((run) => isActiveRunState(run.displayState));
}

export function selectFocusedRun(
  snapshot: StateSnapshotV0 | null,
  focusedRunId: string | null,
): StateSnapshotRun | null {
  if (!snapshot) {
    return null;
  }

  if (focusedRunId) {
    const focusedRun = snapshot.runs.find((run) => run.runId === focusedRunId);
    if (focusedRun) {
      return focusedRun;
    }
  }

  if (snapshot.currentRunId) {
    const currentRun = snapshot.runs.find((run) => run.runId === snapshot.currentRunId);
    if (currentRun) {
      return currentRun;
    }
  }

  return selectActiveRuns(snapshot)[0] ?? snapshot.runs[0] ?? null;
}

export function selectSessionPreview(snapshot: StateSnapshotV0 | null): string {
  if (!snapshot) {
    return "";
  }

  const currentTurn = selectCurrentTurn(snapshot);
  if (currentTurn?.latestMessagePreview) {
    return currentTurn.latestMessagePreview;
  }

  const focusedRun = selectFocusedRun(snapshot, null);
  if (focusedRun?.summary) {
    return focusedRun.summary;
  }

  return focusedRun?.latestMessageText ?? "";
}
