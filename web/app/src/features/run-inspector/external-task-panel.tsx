import type { ExternalTaskSummary } from "../../lib/host-types";

type ExternalTaskPanelProps = {
  externalTasks: ExternalTaskSummary[];
  focusedRunId: string | null;
  onOpenDebug: () => void;
};

export function ExternalTaskPanel({
  externalTasks,
  focusedRunId,
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
      <div className="run-actions">
        <button type="button" disabled>
          Continue
        </button>
        <button type="button" disabled>
          Stop
        </button>
        <button type="button" disabled>
          Add Hint
        </button>
        <button type="button" onClick={onOpenDebug}>
          Debug
        </button>
      </div>
    </section>
  );
}
