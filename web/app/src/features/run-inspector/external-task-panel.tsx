import type { ExternalTaskSummary } from "../../lib/host-types";

type ExternalTaskPanelProps = {
  externalTasks: ExternalTaskSummary[];
};

export function ExternalTaskPanel({ externalTasks }: ExternalTaskPanelProps) {
  return (
    <section className="run-panel">
      <h2>External Task + Actions</h2>
      {externalTasks[0] ? (
        <>
          <p>{externalTasks[0].adapter}</p>
          <p>{externalTasks[0].remoteAgentName}</p>
          <p>{externalTasks[0].state}</p>
          <p>{externalTasks[0].resultSummary || "No result summary yet."}</p>
        </>
      ) : (
        <p>No external task linked.</p>
      )}
      <div className="run-actions">
        <button type="button">Continue</button>
        <button type="button" disabled>
          Stop
        </button>
        <button type="button" disabled>
          Add Hint
        </button>
        <button type="button">Debug</button>
      </div>
    </section>
  );
}
