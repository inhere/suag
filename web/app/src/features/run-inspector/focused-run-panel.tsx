import type {
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
} from "../../lib/host-types";

type FocusedRunPanelProps = {
  focusedRun: StateSnapshotRun | null;
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
};

function getFocusedRunContent(run: StateSnapshotRun | null): string {
  if (!run) {
    return "Waiting for output...";
  }

  return run.summary || run.latestMessageText || "Waiting for output...";
}

type MetaItemProps = {
  label: string;
  value: string;
};

function MetaItem({ label, value }: MetaItemProps) {
  return (
    <div className="run-meta__item">
      <span className="run-meta__label">{label}</span>
      <span className="run-meta__value">{value}</span>
    </div>
  );
}

export function FocusedRunPanel({
  focusedRun,
  externalActivities,
  externalMessages,
}: FocusedRunPanelProps) {
  const focusedRunID = focusedRun?.runId ?? "";
  const runActivities = externalActivities.filter((activity) => activity.runId === focusedRunID);
  const runMessages = externalMessages.filter((message) => message.runId === focusedRunID);

  return (
    <section className="run-panel">
      <h2>Focused Run</h2>
      {focusedRun ? (
        <>
          <div className="run-section">
            <h3>Summary</h3>
            <p className="run-summary">{getFocusedRunContent(focusedRun)}</p>
          </div>

          <div className="run-section">
            <h3>Run Meta</h3>
            <div className="run-meta">
              <MetaItem label="Run" value={focusedRun.runId} />
              <MetaItem label="Agent" value={focusedRun.agentId} />
              <MetaItem label="State" value={focusedRun.displayState} />
              <MetaItem label="Event" value={focusedRun.latestEventType} />
              <MetaItem
                label="External Task"
                value={focusedRun.externalTaskIds.join(", ") || "No external tasks"}
              />
            </div>
          </div>
        </>
      ) : (
        <p className="run-summary">{getFocusedRunContent(focusedRun)}</p>
      )}
      {runMessages.length > 0 ? (
        <div className="run-section">
          <h3>External Messages</h3>
          {runMessages.map((message) => (
            <article key={message.messageId} className="run-detail-card">
              <span className="run-detail-card__status">
                {message.completed ? "completed" : "streaming"}
              </span>
              <p>{message.content}</p>
            </article>
          ))}
        </div>
      ) : null}
      {runActivities.length > 0 ? (
        <div className="run-section">
          <h3>External Activities</h3>
          {runActivities.map((activity) => (
            <article key={activity.activityId} className="run-detail-card">
              <span className="run-detail-card__status">{activity.status}</span>
              <p>{`${activity.title}: ${activity.detail}`}</p>
            </article>
          ))}
        </div>
      ) : null}
    </section>
  );
}
