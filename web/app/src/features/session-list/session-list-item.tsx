import type { SessionListItem as SessionListItemModel } from "../session/session-store";

type SessionListItemProps = {
  item: SessionListItemModel;
  selected: boolean;
  onSelect: () => void;
};

function formatStatusLabel(status: string): string {
  return status.toLowerCase();
}

export function SessionListItem({
  item,
  selected,
  onSelect,
}: SessionListItemProps) {
  return (
    <button
      type="button"
      className={selected ? "session-item session-item--selected" : "session-item"}
      onClick={onSelect}
    >
      <div className="session-item__title">{item.title}</div>
      <div className="session-item__meta">
        {formatStatusLabel(item.status)} · {item.updatedAt}
      </div>
      <div className="session-item__preview">{item.preview}</div>
    </button>
  );
}
