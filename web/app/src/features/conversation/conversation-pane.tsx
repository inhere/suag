import type { StateSnapshotTurn } from "../../lib/host-types";
import { Composer } from "./composer";
import { MessageItem } from "./message-item";
import { TurnSummaryCard } from "./turn-summary-card";

export type ConversationMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
};

type ConversationPaneProps = {
  messages: ConversationMessage[];
  currentTurn: StateSnapshotTurn | null;
  waitingForInput: boolean;
  onSubmit: (message: string) => void;
};

export function ConversationPane({
  messages,
  currentTurn,
  waitingForInput,
  onSubmit,
}: ConversationPaneProps) {
  return (
    <div className="conversation-pane">
      <div className="conversation-pane__content">
        {messages.map((message) => (
          <MessageItem key={message.id} role={message.role} text={message.text} />
        ))}
        {currentTurn ? <TurnSummaryCard turn={currentTurn} /> : null}
      </div>
      <Composer waitingForInput={waitingForInput} onSubmit={onSubmit} />
    </div>
  );
}
