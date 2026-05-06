import { Composer } from "./composer";
import { ActivityItem } from "./activity-item";
import {
  type InlineStatusCardState,
  InlineStatusCard,
} from "./inline-status-card";
import { MessageItem } from "./message-item";
import type { ConversationTimelineEntry } from "./conversation-timeline";

type ConversationPaneProps = {
  messages: ConversationTimelineEntry[];
  waitingForInput: boolean;
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
  inlineStatus = null,
  submitPending = false,
  submitError = null,
  onSubmit,
}: ConversationPaneProps) {
  return (
    <div className="conversation-pane">
      <div className="conversation-pane__content" aria-label="Chat history">
        {messages.map((message) =>
          message.kind === "activity" ? (
            <ActivityItem key={message.id} activity={message} />
          ) : (
            <MessageItem
              key={message.id}
              role={message.role}
              text={message.text}
              timestamp={message.timestamp}
            />
          ),
        )}
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
