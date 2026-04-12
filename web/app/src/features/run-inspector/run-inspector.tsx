import type { ExternalTaskSummary, StateSnapshotRun, StateSnapshotTurn } from "../../lib/host-types";
import { ActiveRunsPanel } from "./active-runs-panel";
import { CurrentTurnPanel } from "./current-turn-panel";
import { ExternalTaskPanel } from "./external-task-panel";
import { FocusedRunPanel } from "./focused-run-panel";

type RunInspectorProps = {
  currentTurn: StateSnapshotTurn | null;
  runs: StateSnapshotRun[];
  externalTasks: ExternalTaskSummary[];
  focusedRunId: string | null;
  onFocusRun: (runId: string) => void;
  onOpenDebug: () => void;
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

export function RunInspector({
  currentTurn,
  runs,
  externalTasks,
  focusedRunId,
  onFocusRun,
  onOpenDebug,
}: RunInspectorProps) {
  const focusedRun = getFocusedRun(runs, focusedRunId);

  return (
    <div className="run-inspector">
      <CurrentTurnPanel currentTurn={currentTurn} />
      <ActiveRunsPanel
        runs={runs}
        focusedRunId={focusedRun?.runId ?? null}
        onFocusRun={onFocusRun}
      />
      <FocusedRunPanel focusedRun={focusedRun} />
      <ExternalTaskPanel externalTasks={externalTasks} onOpenDebug={onOpenDebug} />
    </div>
  );
}
