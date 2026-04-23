import type { StateSnapshotTurn } from "../../lib/host-types";
import { ChatHeader } from "./chat-header";
import { Composer } from "./composer";
import {
  type InlineStatusCardState,
  InlineStatusCard,
} from "./inline-status-card";
import { MessageItem } from "./message-item";
import { SummaryStrip, type SummaryStripTone } from "./summary-strip";

export type ConversationMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  timestamp?: string;
};

type ConversationPaneProps = {
  messages: ConversationMessage[];
  waitingForInput: boolean;
  headerTitle?: string;
  headerMeta?: string | null;
  summaryStatus: string;
  summaryTone: SummaryStripTone;
  inlineStatus?: {
    state: InlineStatusCardState;
    detail?: string | null;
  } | null;
  submitPending?: boolean;
  submitError?: string | null;
  onSubmit: (message: string) => void;
};

export function ConversationPane({
  messages,
  waitingForInput,
  headerTitle = "Chat",
  headerMeta = null,
  summaryStatus,
  summaryTone,
  inlineStatus = null,
  submitPending = false,
  submitError = null,
  onSubmit,
}: ConversationPaneProps) {
  return (
    <div className="conversation-pane">
      <ChatHeader title={headerTitle} meta={headerMeta} />
      <SummaryStrip status={summaryStatus} tone={summaryTone} />
      <div className="conversation-pane__content">
        {messages.map((message) => (
          <MessageItem
            key={message.id}
            role={message.role}
            text={message.text}
            timestamp={message.timestamp}
          />
        ))}
        {inlineStatus ? (
          <InlineStatusCard
            state={inlineStatus.state}
            detail={inlineStatus.detail ?? null}
          />
        ) : null}
      </div>
      <Composer
        waitingForInput={waitingForInput}
        submitPending={submitPending}
        submitError={submitError}
        onSubmit={onSubmit}
      />
    </div>
  );
}
