import type { ConversationMessage } from "../features/conversation/conversation-pane";
import { ConversationPane } from "../features/conversation/conversation-pane";
import { DebugDrawer } from "../features/debug/debug-drawer";
import { RunInspector } from "../features/run-inspector/run-inspector";
import { SessionList } from "../features/session-list/session-list";
import type { SessionFilter, SessionListItem } from "../features/session/session-store";
import type {
  ExternalTaskSummary,
  SessionSocketMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
} from "../lib/host-types";

type AppShellProps = {
  sessions?: SessionListItem[];
  currentSessionId?: string | null;
  filter?: SessionFilter;
  searchQuery?: string;
  messages?: ConversationMessage[];
  currentTurn?: StateSnapshotTurn | null;
  waitingForInput?: boolean;
  runs?: StateSnapshotRun[];
  externalTasks?: ExternalTaskSummary[];
  focusedRunId?: string | null;
  debugOpen?: boolean;
  debugEvents?: SessionSocketMessage[];
  onCreateSession?: () => void;
  onSelectSession?: (sessionId: string) => void;
  onSearchChange?: (value: string) => void;
  onFilterChange?: (filter: SessionFilter) => void;
  onSubmitMessage?: (message: string) => void;
  onFocusRun?: (runId: string) => void;
  onOpenDebug?: () => void;
  onCloseDebug?: () => void;
};

export function AppShell({
  sessions = [],
  currentSessionId = null,
  filter = "all",
  searchQuery = "",
  messages = [],
  currentTurn = null,
  waitingForInput = false,
  runs = [],
  externalTasks = [],
  focusedRunId = null,
  debugOpen = false,
  debugEvents = [],
  onCreateSession = () => {},
  onSelectSession = () => {},
  onSearchChange = () => {},
  onFilterChange = () => {},
  onSubmitMessage = () => {},
  onFocusRun = () => {},
  onOpenDebug = () => {},
  onCloseDebug = () => {},
}: AppShellProps) {

  return (
    <>
      <div className="app-shell">
        <aside className="app-shell__panel" aria-label="Sessions panel">
        <SessionList
          sessions={sessions}
          currentSessionId={currentSessionId}
          filter={filter}
          searchQuery={searchQuery}
          onCreateSession={onCreateSession}
          onSelect={onSelectSession}
          onSearchChange={onSearchChange}
          onFilterChange={onFilterChange}
        />
        </aside>
        <main className="app-shell__panel" aria-label="Conversation panel">
          <h1>Conversation</h1>
          <ConversationPane
            messages={messages}
            currentTurn={currentTurn}
            waitingForInput={waitingForInput}
            onSubmit={onSubmitMessage}
          />
        </main>
        <section className="app-shell__panel" aria-label="Run Inspector panel">
          <h1>Run Inspector</h1>
          <RunInspector
            currentTurn={currentTurn}
            runs={runs}
            externalTasks={externalTasks}
            focusedRunId={focusedRunId}
            onFocusRun={onFocusRun}
            onOpenDebug={onOpenDebug}
          />
        </section>
      </div>
      <DebugDrawer open={debugOpen} events={debugEvents} onClose={onCloseDebug} />
    </>
  );
}
