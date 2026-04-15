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
  focusedRunDetailStatus?: "idle" | "loading" | "loaded" | "error";
  onFocusRun: (runId: string) => void;
  onRetryFocusedRunDetail?: () => void;
  canStop: boolean;
  stopPending?: boolean;
  onStop: () => void;
  canAddHint: boolean;
  waitingForApproval?: boolean;
  addHintPending?: boolean;
  actionError?: string | null;
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
  focusedRunDetailStatus = "idle",
  onFocusRun,
  onRetryFocusedRunDetail = () => {},
  canStop,
  stopPending = false,
  onStop,
  canAddHint,
  waitingForApproval = false,
  addHintPending = false,
  actionError = null,
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
        detailStatus={focusedRunDetailStatus}
        onRetryDetail={onRetryFocusedRunDetail}
      />
      <ExternalTaskPanel
        externalTasks={externalTasks}
        focusedRunId={focusedRun?.runId ?? null}
        canStop={canStop}
        stopPending={stopPending}
        onStop={onStop}
        canAddHint={canAddHint}
        waitingForApproval={waitingForApproval}
        addHintPending={addHintPending}
        actionError={actionError}
        onAddHint={onAddHint}
        onOpenDebug={onOpenDebug}
      />
    </div>
  );
}
