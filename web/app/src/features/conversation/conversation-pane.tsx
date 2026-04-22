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
  currentTurn: StateSnapshotTurn | null;
  waitingForInput: boolean;
  submitPending?: boolean;
  submitError?: string | null;
  onSubmit: (message: string) => void;
};

function normalizeState(value: string | undefined): string {
  return (value ?? "").toLowerCase();
}

function deriveSummaryTone(displayState: string): SummaryStripTone {
  if (displayState.includes("failed") || displayState.includes("error")) {
    return "failed";
  }

  if (displayState.includes("cancel")) {
    return "attention";
  }

  if (
    displayState.includes("input_required") ||
    displayState.includes("auth_required") ||
    displayState.includes("waiting")
  ) {
    return "waiting";
  }

  if (displayState.includes("running") || displayState.includes("dispatch")) {
    return "running";
  }

  return "attention";
}

function deriveSummaryStatus(turn: StateSnapshotTurn | null): string {
  if (!turn) {
    return "尚未开始，发送一条消息创建新的 turn。";
  }

  if (turn.latestMessagePreview) {
    return turn.latestMessagePreview;
  }

  return `Turn ${turn.turnId} · ${turn.displayState.replaceAll("_", " ")}`;
}

function deriveInlineState(displayState: string): InlineStatusCardState | null {
  if (displayState.includes("auth_required")) {
    return "auth_required";
  }

  if (
    displayState.includes("input_required") ||
    displayState.includes("waiting_input")
  ) {
    return "input_required";
  }

  return null;
}

export function ConversationPane({
  messages,
  currentTurn,
  waitingForInput,
  submitPending = false,
  submitError = null,
  onSubmit,
}: ConversationPaneProps) {
  const displayState = normalizeState(currentTurn?.displayState);
  const summaryTone = deriveSummaryTone(displayState);
  const summaryStatus = deriveSummaryStatus(currentTurn);
  const inlineState = deriveInlineState(displayState);

  return (
    <div className="conversation-pane">
      <ChatHeader
        title="Chat"
        meta={currentTurn ? `Turn ${currentTurn.turnId} · ${currentTurn.displayState}` : "Ready"}
      />
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
        {inlineState ? (
          <InlineStatusCard
            state={inlineState}
            detail={currentTurn?.latestMessagePreview || null}
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
