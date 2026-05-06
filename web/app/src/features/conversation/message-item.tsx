import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

type MessageItemProps = {
  role: "user" | "assistant";
  text: string;
  timestamp?: string;
};

const markdownPlugins = [remarkGfm];

export function MessageItem({ role, text, timestamp }: MessageItemProps) {
  const isAssistant = role === "assistant";

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
          {isAssistant ? "Axon" : "You"}
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
    </article>
  );
}
