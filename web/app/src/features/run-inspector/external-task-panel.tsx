import type { ExternalTaskSummary } from "../../lib/host-types";

type ExternalTaskPanelProps = {
  externalTasks: ExternalTaskSummary[];
  focusedRunId: string | null;
  canStop: boolean;
  stopPending?: boolean;
  onStop: () => void;
  canAddHint: boolean;
  addHintPending?: boolean;
  actionError?: string | null;
  onAddHint: () => void;
  onOpenDebug: () => void;
};

export function ExternalTaskPanel({
  externalTasks,
  focusedRunId,
  canStop,
  stopPending = false,
  onStop,
  canAddHint,
  addHintPending = false,
  actionError = null,
  onAddHint,
  onOpenDebug,
}: ExternalTaskPanelProps) {
  const focusedTask =
    externalTasks.find((task) => task.runId === focusedRunId) ?? externalTasks[0] ?? null;

  return (
    <section className="run-panel">
      <h2>External Task + Actions</h2>
      {focusedTask ? (
        <>
          <p>{focusedTask.adapter}</p>
          <p>{focusedTask.remoteAgentName}</p>
          <p>{focusedTask.state}</p>
          <p>{focusedTask.resultSummary || "No result summary yet."}</p>
        </>
      ) : (
        <p>No external task linked.</p>
      )}
      {stopPending ? <p className="action-status">正在停止当前 turn...</p> : null}
      {addHintPending ? <p className="action-status">正在追加提示...</p> : null}
      {actionError ? <p className="inline-feedback inline-feedback--error">{actionError}</p> : null}
      <div className="run-actions">
        <button type="button" disabled>
          Continue
        </button>
        <button type="button" disabled={!canStop || stopPending} onClick={onStop}>
          {stopPending ? "Stopping..." : "Stop"}
        </button>
        <button type="button" disabled={!canAddHint || addHintPending} onClick={onAddHint}>
          {addHintPending ? "Adding Hint..." : "Add Hint"}
        </button>
        <button type="button" onClick={onOpenDebug}>
          Debug
        </button>
      </div>
    </section>
  );
}
