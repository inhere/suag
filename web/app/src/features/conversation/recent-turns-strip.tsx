import type { StateSnapshotTurn } from "../../lib/host-types";

type RecentTurnsStripProps = {
  turns: StateSnapshotTurn[];
  currentTurnId?: string | null;
  onSelectTurn: (turn: StateSnapshotTurn) => void;
};

function buildTurnMeta(turn: StateSnapshotTurn): string {
  return `${turn.displayState} · ${turn.runCount} runs`;
}

export function RecentTurnsStrip({
  turns,
  currentTurnId = null,
  onSelectTurn,
}: RecentTurnsStripProps) {
  if (!turns.length) {
    return null;
  }

  return (
    <section className="recent-turns">
      <div className="recent-turns__header">
        <h2>Recent Turns</h2>
        <span>{`${turns.length} shown`}</span>
      </div>
      <div className="recent-turns__list">
        {turns.map((turn) => (
          <button
            key={turn.turnId}
            type="button"
            className={
              turn.turnId === currentTurnId
                ? "recent-turns__item recent-turns__item--active"
                : "recent-turns__item"
            }
            onClick={() => onSelectTurn(turn)}
          >
            <strong>{`Turn ${turn.turnId}`}</strong>
            <span>{buildTurnMeta(turn)}</span>
            <span>{turn.latestMessagePreview || "No summary yet."}</span>
          </button>
        ))}
      </div>
    </section>
  );
}
