type ChatHeaderProps = {
  title?: string;
  meta?: string | null;
};

export function ChatHeader({
  title = "Chat",
  meta = null,
}: ChatHeaderProps) {
  return (
    <header className="chat-header">
      <h1 className="chat-header__title">{title}</h1>
      {meta ? <p className="chat-header__meta">{meta}</p> : null}
    </header>
  );
}
