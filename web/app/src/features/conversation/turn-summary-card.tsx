import type { StateSnapshotTurn } from "../../lib/host-types";

type TurnSummaryCardProps = {
  turn: StateSnapshotTurn;
};

export function TurnSummaryCard({ turn }: TurnSummaryCardProps) {
  return (
    <section className="turn-summary-card">
      <strong>{`Turn ${turn.turnId}`}</strong>
      <p>{`${turn.displayState} · ${turn.activeRunCount} active runs`}</p>
      <p>{turn.latestMessagePreview}</p>
    </section>
  );
}
