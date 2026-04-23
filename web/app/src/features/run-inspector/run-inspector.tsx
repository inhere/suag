import { RunDetailsCompactPanel } from "../context/run-details-compact-panel";
import { SessionSummaryPanel } from "../context/session-summary-panel";
import type { StateSnapshotRun, StateSnapshotTurn } from "../../lib/host-types";

type RunInspectorProps = {
  currentTurn: StateSnapshotTurn | null;
  runs: StateSnapshotRun[];
  focusedRunId?: string | null;
};

export function RunInspector({
  currentTurn,
  runs,
  focusedRunId = null,
}: RunInspectorProps) {
  return (
    <div className="run-inspector run-inspector--compact">
      <SessionSummaryPanel currentTurn={currentTurn} />
      <RunDetailsCompactPanel runs={runs} focusedRunId={focusedRunId} />
    </div>
  );
}
