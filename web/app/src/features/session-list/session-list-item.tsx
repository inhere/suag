import type { SessionListItem as SessionListItemModel } from "../session/session-store";

type SessionListItemProps = {
  item: SessionListItemModel;
  selected: boolean;
  onSelect: () => void;
};

export type SessionTone = "idle" | "running" | "waiting" | "attention";

export function mapStatusToTone(status: string): SessionTone {
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

export function formatSessionStatus(status: string): string {
  return status.toLowerCase().replaceAll("_", " ");
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
        <span className="session-item__status">{formatSessionStatus(item.status)}</span>
        <span aria-hidden="true">·</span>
        <span>{item.updatedAt}</span>
      </div>
    </button>
  );
}
