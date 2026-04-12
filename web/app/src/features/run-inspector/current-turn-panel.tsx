import type { StateSnapshotTurn } from "../../lib/host-types";

type CurrentTurnPanelProps = {
  currentTurn: StateSnapshotTurn | null;
};

export function CurrentTurnPanel({ currentTurn }: CurrentTurnPanelProps) {
  if (!currentTurn) {
    return (
      <section className="run-panel">
        <h2>Current Turn</h2>
        <p>No active turn selected.</p>
      </section>
    );
  }

  return (
    <section className="run-panel">
      <h2>Current Turn</h2>
      <p>{currentTurn.displayState}</p>
      <p>{`${currentTurn.runCount} runs · ${currentTurn.activeRunCount} active`}</p>
      <p>{currentTurn.latestMessagePreview}</p>
    </section>
  );
}
