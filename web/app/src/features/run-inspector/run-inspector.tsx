import type {
  ExternalTaskSummary,
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
} from "../../lib/host-types";
import { ActiveRunsPanel } from "./active-runs-panel";
import { CurrentTurnPanel } from "./current-turn-panel";
import { ExternalTaskPanel } from "./external-task-panel";
import { FocusedRunPanel } from "./focused-run-panel";

type RunInspectorProps = {
  currentTurn: StateSnapshotTurn | null;
  runs: StateSnapshotRun[];
  externalTasks: ExternalTaskSummary[];
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
  focusedRunId: string | null;
  onFocusRun: (runId: string) => void;
  canStop: boolean;
  onStop: () => void;
  canAddHint: boolean;
  onAddHint: () => void;
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
  externalActivities,
  externalMessages,
  focusedRunId,
  onFocusRun,
  canStop,
  onStop,
  canAddHint,
  onAddHint,
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
      <FocusedRunPanel
        focusedRun={focusedRun}
        externalActivities={externalActivities}
        externalMessages={externalMessages}
      />
      <ExternalTaskPanel
        externalTasks={externalTasks}
        focusedRunId={focusedRun?.runId ?? null}
        canStop={canStop}
        onStop={onStop}
        canAddHint={canAddHint}
        onAddHint={onAddHint}
        onOpenDebug={onOpenDebug}
      />
    </div>
  );
}
