import type { StateSnapshotTurn } from "../../lib/host-types";
import { Composer } from "./composer";
import { MessageItem } from "./message-item";
import { RecentTurnsStrip } from "./recent-turns-strip";
import { TurnSummaryCard } from "./turn-summary-card";

export type ConversationMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
};

type ConversationPaneProps = {
  messages: ConversationMessage[];
  currentTurn: StateSnapshotTurn | null;
  recentTurns?: StateSnapshotTurn[];
  onSelectTurn?: (turn: StateSnapshotTurn) => void;
  waitingForInput: boolean;
  submitPending?: boolean;
  onSubmit: (message: string) => void;
};

export function ConversationPane({
  messages,
  currentTurn,
  recentTurns = [],
  onSelectTurn = () => {},
  waitingForInput,
  submitPending = false,
  onSubmit,
}: ConversationPaneProps) {
  return (
    <div className="conversation-pane">
      <div className="conversation-pane__content">
        {messages.map((message) => (
          <MessageItem key={message.id} role={message.role} text={message.text} />
        ))}
        {currentTurn ? <TurnSummaryCard turn={currentTurn} /> : null}
        <RecentTurnsStrip
          turns={recentTurns}
          currentTurnId={currentTurn?.turnId ?? null}
          onSelectTurn={onSelectTurn}
        />
      </div>
      <Composer
        waitingForInput={waitingForInput}
        submitPending={submitPending}
        onSubmit={onSubmit}
      />
    </div>
  );
}
