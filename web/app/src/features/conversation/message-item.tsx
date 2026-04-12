type MessageItemProps = {
  role: "user" | "assistant";
  text: string;
};

export function MessageItem({ role, text }: MessageItemProps) {
  return (
    <article
      className={
        role === "user"
          ? "conversation-message conversation-message--user"
          : "conversation-message conversation-message--assistant"
      }
    >
      <div className="conversation-message__role">
        {role === "user" ? "You" : "Axon"}
      </div>
      <div className="conversation-message__text">{text}</div>
    </article>
  );
}
