import type { StateSnapshotRun } from "../../lib/host-types";

type RunDetailsCompactPanelProps = {
  runs: StateSnapshotRun[];
  focusedRunId?: string | null;
};

function getFocusedRun(
  runs: StateSnapshotRun[],
  focusedRunId: string | null,
): StateSnapshotRun | null {
  if (!runs.length) {
    return null;
  }

  return runs.find((run) => run.runId === focusedRunId) ?? runs[0] ?? null;
}

function getWaitKind(run: StateSnapshotRun | null): string {
  const value = run?.waitMetadata?.wait_kind;
  if (typeof value === "string" && value.trim()) {
    return value;
  }
  return "none";
}

function getRunSummary(run: StateSnapshotRun | null): string {
  if (!run) {
    return "No focused run.";
  }

  if (run.displayState === "failed" && run.latestMessageText.trim()) {
    return run.latestMessageText;
  }

  if (run.displayState === "completed" && run.summary.trim()) {
    return run.summary;
  }

  if (run.summary.trim()) {
    return run.summary;
  }

  if (run.latestMessageText.trim()) {
    return run.latestMessageText;
  }

  return "No run summary yet.";
}

export function RunDetailsCompactPanel({
  runs,
  focusedRunId = null,
}: RunDetailsCompactPanelProps) {
  const focusedRun = getFocusedRun(runs, focusedRunId);

  return (
    <section className="context-panel" aria-label="Run details panel">
      <div className="context-panel__header">
        <h2>Run details</h2>
        <span>{focusedRun?.displayState ?? "idle"}</span>
      </div>
      <div className="context-panel__meta">
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Run</span>
          <span className="context-panel__value">{focusedRun?.runId ?? "none"}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Agent</span>
          <span className="context-panel__value">{focusedRun?.agentId ?? "none"}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Status</span>
          <span className="context-panel__value">{focusedRun?.displayState ?? "idle"}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Wait kind</span>
          <span className="context-panel__value">{getWaitKind(focusedRun)}</span>
        </div>
      </div>
      <p className="inline-feedback">{getRunSummary(focusedRun)}</p>
    </section>
  );
}
