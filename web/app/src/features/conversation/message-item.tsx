type MessageItemProps = {
  role: "user" | "assistant";
  text: string;
  timestamp?: string;
};

export function MessageItem({ role, text, timestamp }: MessageItemProps) {
  return (
    <article
      className={
        role === "user"
          ? "conversation-message conversation-message--user"
          : "conversation-message conversation-message--assistant"
      }
    >
      <div className="conversation-message__meta">
        <div className="conversation-message__role">
          {role === "user" ? "You" : "Axon"}
        </div>
        {timestamp ? <time className="conversation-message__time">{timestamp}</time> : null}
      </div>
      <div className="conversation-message__text">{text}</div>
    </article>
  );
}
