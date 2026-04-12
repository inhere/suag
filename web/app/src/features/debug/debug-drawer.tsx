import type { SessionSocketMessage } from "../../lib/host-types";

type DebugDrawerProps = {
  open: boolean;
  events: SessionSocketMessage[];
  onClose: () => void;
};

function getEventKey(event: SessionSocketMessage, index: number): string {
  if ("sequence" in event && typeof event.sequence === "number") {
    return `${event.type}:${event.sequence}`;
  }

  if ("messageId" in event && typeof event.messageId === "string") {
    return `${event.type}:${event.messageId}`;
  }

  return `${event.type}:${index}`;
}

export function DebugDrawer({ open, events, onClose }: DebugDrawerProps) {
  if (!open) {
    return null;
  }

  return (
    <aside className="debug-drawer" aria-label="Debug drawer">
      <div className="debug-drawer__header">
        <h2>Debug Events</h2>
        <button type="button" onClick={onClose}>
          Close
        </button>
      </div>
      <div className="debug-drawer__content">
        {events.length ? (
          events.map((event, index) => (
            <pre key={getEventKey(event, index)} className="debug-drawer__event">
              {JSON.stringify(event, null, 2)}
            </pre>
          ))
        ) : (
          <p>No raw events captured yet.</p>
        )}
      </div>
    </aside>
  );
}
