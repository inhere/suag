import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { TokenUsage } from "./conversation-timeline";

type MessageItemProps = {
  role: "user" | "assistant";
  text: string;
  timestamp?: string;
  tokenUsage?: TokenUsage | null;
};

const markdownPlugins = [remarkGfm];

export function MessageItem({ role, text, timestamp, tokenUsage = null }: MessageItemProps) {
  const isAssistant = role === "assistant";
  const tokenUsageLabel = isAssistant ? formatTokenUsage(tokenUsage) : "";

  return (
    <article
      className={
        isAssistant
          ? "conversation-message conversation-message--assistant"
          : "conversation-message conversation-message--user"
      }
    >
      <div className="conversation-message__meta">
        <div className="conversation-message__role">
          {isAssistant ? "Suag" : "You"}
        </div>
        {timestamp ? <time className="conversation-message__time">{timestamp}</time> : null}
      </div>
      <div className="conversation-message__text">
        {isAssistant ? (
          <ReactMarkdown remarkPlugins={markdownPlugins}>{text}</ReactMarkdown>
        ) : (
          text
        )}
      </div>
      {tokenUsageLabel ? (
        <div className="conversation-message__usage">{tokenUsageLabel}</div>
      ) : null}
    </article>
  );
}

function formatTokenUsage(usage: TokenUsage | null): string {
  if (!usage) {
    return "";
  }

  const parts: string[] = [];
  if (usage.inputTokens != null) {
    parts.push(`input ${formatNumber(usage.inputTokens)}`);
  }
  if (usage.outputTokens != null) {
    parts.push(`output ${formatNumber(usage.outputTokens)}`);
  }
  if (usage.totalTokens != null) {
    parts.push(`total ${formatNumber(usage.totalTokens)}`);
  }
  return parts.length ? `Tokens · ${parts.join(" · ")}` : "";
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat("en-US").format(value);
}
