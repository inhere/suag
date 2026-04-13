import type { SessionSocketStatus } from "../../lib/session-ws";
import type { SessionFilter, SessionListItem as SessionListItemModel } from "../session/session-store";
import { SessionListItem } from "./session-list-item";

type SessionListProps = {
  sessions: SessionListItemModel[];
  currentSessionId: string | null;
  connectionStatus?: SessionSocketStatus | "idle";
  statusMessage?: string;
  errorMessage?: string | null;
  onRetry?: () => void;
  filter: SessionFilter;
  searchQuery: string;
  onCreateSession: () => void;
  onSelect: (sessionId: string) => void;
  onSearchChange: (value: string) => void;
  onFilterChange: (filter: SessionFilter) => void;
};

function matchesFilter(status: string, filter: SessionFilter): boolean {
  if (filter === "all") {
    return true;
  }

  return status.toLowerCase() === filter;
}

export function SessionList({
  sessions,
  currentSessionId,
  connectionStatus = "idle",
  statusMessage = "No active session selected.",
  errorMessage = null,
  onRetry = () => {},
  filter,
  searchQuery,
  onCreateSession,
  onSelect,
  onSearchChange,
  onFilterChange,
}: SessionListProps) {
  const normalizedQuery = searchQuery.trim().toLowerCase();
  const visibleSessions = sessions.filter((session) => {
    if (!matchesFilter(session.status, filter)) {
      return false;
    }

    if (!normalizedQuery) {
      return true;
    }

    return (
      session.title.toLowerCase().includes(normalizedQuery) ||
      session.preview.toLowerCase().includes(normalizedQuery)
    );
  });

  return (
    <div className="session-list">
      <div className="session-list__header">
        <h1>Sessions</h1>
        <button type="button" className="session-list__new-button" onClick={onCreateSession}>
          + New Session
        </button>
      </div>

      <div
        className={`session-list__status session-list__status--${connectionStatus}`}
        aria-live="polite"
      >
        <span className="session-list__status-dot" aria-hidden="true" />
        <span>{statusMessage}</span>
      </div>
      {errorMessage ? (
        <div className="session-list__error" role="alert">
          <span>{errorMessage}</span>
          <button type="button" className="session-list__retry" onClick={onRetry}>
            Retry
          </button>
        </div>
      ) : null}

      <input
        className="session-list__search"
        placeholder="Search sessions"
        value={searchQuery}
        onChange={(event) => onSearchChange(event.target.value)}
      />

      <div className="session-list__filters" role="group" aria-label="Session filters">
        {(["all", "running", "waiting"] as SessionFilter[]).map((option) => (
          <button
            key={option}
            type="button"
            className={
              option === filter
                ? "session-list__filter session-list__filter--active"
                : "session-list__filter"
            }
            onClick={() => onFilterChange(option)}
          >
            {option === "all" ? "All" : option === "running" ? "Running" : "Waiting"}
          </button>
        ))}
      </div>

      <div className="session-list__items">
        {visibleSessions.map((session) => (
          <SessionListItem
            key={session.sessionId}
            item={session}
            selected={session.sessionId === currentSessionId}
            onSelect={() => onSelect(session.sessionId)}
          />
        ))}
      </div>
    </div>
  );
}
