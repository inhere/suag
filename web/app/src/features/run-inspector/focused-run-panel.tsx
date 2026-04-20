import type {
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
} from "../../lib/host-types";

type FocusedRunPanelProps = {
  focusedRun: StateSnapshotRun | null;
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
  detailStatus?: "idle" | "loading" | "loaded" | "error";
  onRetryDetail?: () => void;
};

function getFocusedRunContent(run: StateSnapshotRun | null): string {
  if (!run) {
    return "Waiting for output...";
  }

  return run.summary || run.latestMessageText || "Waiting for output...";
}

function getWaitMetadataEntries(run: StateSnapshotRun | null): Array<[string, string]> {
  if (!run) {
    return [];
  }

  return Object.entries(run.waitMetadata ?? {})
    .filter(([, value]) => value !== undefined && value !== null && `${value}`.trim() !== "")
    .map(([key, value]) => [key, typeof value === "string" ? value : JSON.stringify(value)]);
}

function getMemoryRecallEntries(run: StateSnapshotRun | null): Array<[string, string]> {
  if (!run) {
    return [];
  }

  return Object.entries(run.memoryRecall ?? {})
    .filter(([, value]) => value !== undefined && value !== null && `${value}`.trim() !== "")
    .map(([key, value]) => [key, typeof value === "string" ? value : JSON.stringify(value)]);
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
  detailStatus = "idle",
  onRetryDetail = () => {},
}: FocusedRunPanelProps) {
  const focusedRunID = focusedRun?.runId ?? "";
  const runActivities = externalActivities.filter((activity) => activity.runId === focusedRunID);
  const runMessages = externalMessages.filter((message) => message.runId === focusedRunID);
  const waitMetadataEntries = getWaitMetadataEntries(focusedRun);
  const memoryRecallEntries = getMemoryRecallEntries(focusedRun);
  const showLoadingState = !!focusedRun && detailStatus === "loading" && !runMessages.length && !runActivities.length;
  const showErrorState = !!focusedRun && detailStatus === "error" && !runMessages.length && !runActivities.length;

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

          {waitMetadataEntries.length > 0 ? (
            <div className="run-section">
              <h3>Wait Info</h3>
              <div className="run-meta">
                {waitMetadataEntries.map(([key, value]) => (
                  <MetaItem key={key} label={key} value={value} />
                ))}
              </div>
            </div>
          ) : null}

          {memoryRecallEntries.length > 0 ? (
            <div className="run-section">
              <h3>Memory Recall</h3>
              <div className="run-meta">
                {memoryRecallEntries.map(([key, value]) => (
                  <MetaItem key={key} label={key} value={value} />
                ))}
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <p className="run-summary">{getFocusedRunContent(focusedRun)}</p>
      )}
      {showLoadingState ? (
        <div className="run-section">
          <h3>Run Detail</h3>
          <p className="run-detail-hint">Loading run detail...</p>
        </div>
      ) : null}
      {showErrorState ? (
        <div className="run-section">
          <h3>Run Detail</h3>
          <div className="run-detail-error" role="alert">
            <span>Failed to load run detail.</span>
            <button type="button" className="run-detail-error__retry" onClick={onRetryDetail}>
              Retry Detail
            </button>
          </div>
        </div>
      ) : null}
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
