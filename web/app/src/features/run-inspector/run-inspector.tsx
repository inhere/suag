import { RunDetailsCompactPanel } from "../context/run-details-compact-panel";
import { SessionSummaryPanel } from "../context/session-summary-panel";
import type {
  ExternalTaskSummary,
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
} from "../../lib/host-types";

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

export function RunInspector({
  currentTurn,
  runs,
  externalTasks: _externalTasks,
  externalActivities: _externalActivities,
  externalMessages: _externalMessages,
  focusedRunId,
  focusedRunDetailStatus: _focusedRunDetailStatus = "idle",
  onFocusRun: _onFocusRun,
  onRetryFocusedRunDetail: _onRetryFocusedRunDetail = () => {},
  canStop: _canStop,
  stopPending: _stopPending = false,
  onStop: _onStop,
  canAddHint: _canAddHint,
  waitingForApproval: _waitingForApproval = false,
  addHintPending: _addHintPending = false,
  actionError: _actionError = null,
  onAddHint: _onAddHint,
  onOpenDebug: _onOpenDebug,
}: RunInspectorProps) {
  return (
    <div className="run-inspector run-inspector--compact">
      <SessionSummaryPanel currentTurn={currentTurn} />
      <RunDetailsCompactPanel runs={runs} focusedRunId={focusedRunId} />
    </div>
  );
}
