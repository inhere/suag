import type { StateSnapshotTurn } from "../../lib/host-types";

type SessionSummaryPanelProps = {
  currentTurn: StateSnapshotTurn | null;
};

function getSessionStatus(currentTurn: StateSnapshotTurn | null): string {
  return currentTurn?.displayState ?? "idle";
}

export function SessionSummaryPanel({ currentTurn }: SessionSummaryPanelProps) {
  const status = getSessionStatus(currentTurn);

  return (
    <section className="context-panel" aria-label="Session summary panel">
      <div className="context-panel__header">
        <h2>Session summary</h2>
        <span>{status}</span>
      </div>
      <div className="context-panel__meta">
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Status</span>
          <span className="context-panel__value">{status}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Turn</span>
          <span className="context-panel__value">{currentTurn?.turnId ?? "none"}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Active runs</span>
          <span className="context-panel__value">{currentTurn?.activeRunCount ?? 0}</span>
        </div>
        <div className="context-panel__meta-item">
          <span className="context-panel__label">Updated</span>
          <span className="context-panel__value">{currentTurn?.updatedAt ?? "-"}</span>
        </div>
      </div>
    </section>
  );
}
