import type { SessionListItem as SessionListItemModel } from "../session/session-store";

type SessionListItemProps = {
  item: SessionListItemModel;
  selected: boolean;
  onSelect: () => void;
};

type SessionTone = "idle" | "running" | "waiting" | "attention";

function mapStatusToTone(status: string): SessionTone {
  const normalizedStatus = status.toLowerCase();
  if (normalizedStatus.includes("running") || normalizedStatus.includes("dispatch")) {
    return "running";
  }

  if (
    normalizedStatus.includes("input_required") ||
    normalizedStatus.includes("auth_required") ||
    normalizedStatus.includes("waiting")
  ) {
    return "waiting";
  }

  if (
    normalizedStatus.includes("failed") ||
    normalizedStatus.includes("error") ||
    normalizedStatus.includes("cancel")
  ) {
    return "attention";
  }

  return "idle";
}

export function SessionListItem({
  item,
  selected,
  onSelect,
}: SessionListItemProps) {
  const tone = mapStatusToTone(item.status);

  return (
    <button
      type="button"
      className={selected ? "session-item session-item--selected" : "session-item"}
      onClick={onSelect}
    >
      <div className="session-item__title">{item.title}</div>
      <div className="session-item__preview">{item.preview}</div>
      <div className="session-item__meta">
        <span className={`session-item__dot session-item__dot--${tone}`} aria-hidden="true" />
        <span>{item.updatedAt}</span>
      </div>
    </button>
  );
}
