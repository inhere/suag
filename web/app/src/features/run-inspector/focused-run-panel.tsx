import type { StateSnapshotRun } from "../../lib/host-types";

type FocusedRunPanelProps = {
  focusedRun: StateSnapshotRun | null;
};

function getFocusedRunContent(run: StateSnapshotRun | null): string {
  if (!run) {
    return "Waiting for output...";
  }

  return run.summary || run.latestMessageText || "Waiting for output...";
}

export function FocusedRunPanel({ focusedRun }: FocusedRunPanelProps) {
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
    </section>
  );
}
