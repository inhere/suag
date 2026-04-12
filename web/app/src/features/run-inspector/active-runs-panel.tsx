import type { StateSnapshotRun } from "../../lib/host-types";

type ActiveRunsPanelProps = {
  runs: StateSnapshotRun[];
  focusedRunId: string | null;
  onFocusRun: (runId: string) => void;
};

function getRunPreview(run: StateSnapshotRun): string {
  return run.summary || run.latestMessageText || "Waiting for output...";
}

export function ActiveRunsPanel({
  runs,
  focusedRunId,
  onFocusRun,
}: ActiveRunsPanelProps) {
  return (
    <section className="run-panel">
      <h2>Active Runs</h2>
      <div className="run-list">
        {runs.map((run) => (
          <button
            key={run.runId}
            type="button"
            className={
              run.runId === focusedRunId ? "run-chip run-chip--active" : "run-chip"
            }
            onClick={() => onFocusRun(run.runId)}
          >
            <strong>{run.agentId}</strong>
            <span>{run.displayState}</span>
            <span>{getRunPreview(run)}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
