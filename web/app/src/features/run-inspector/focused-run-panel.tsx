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
          <p>{focusedRun.runId}</p>
          <p>{focusedRun.agentId}</p>
          <p>{focusedRun.displayState}</p>
          <p>{focusedRun.latestEventType}</p>
          <p>{focusedRun.externalTaskIds.join(", ") || "No external tasks"}</p>
        </>
      ) : null}
      <p>{getFocusedRunContent(focusedRun)}</p>
      {runMessages.length > 0 ? (
        <>
          <p>External Messages</p>
          {runMessages.map((message) => (
            <p key={message.messageId}>{message.content}</p>
          ))}
        </>
      ) : null}
      {runActivities.length > 0 ? (
        <>
          <p>External Activities</p>
          {runActivities.map((activity) => (
            <p key={activity.activityId}>{`${activity.title}: ${activity.detail}`}</p>
          ))}
        </>
      ) : null}
    </section>
  );
}
