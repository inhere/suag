import type { ConversationMessage } from "../features/conversation/conversation-pane";
import { ConversationPane } from "../features/conversation/conversation-pane";
import { DebugDrawer } from "../features/debug/debug-drawer";
import { HITLPanel } from "../features/hitl/hitl-panel";
import { RunInspector } from "../features/run-inspector/run-inspector";
import { SessionList } from "../features/session-list/session-list";
import type { SessionFilter, SessionListItem } from "../features/session/session-store";
import type {
  ExternalTaskSummary,
  HITLTicket,
  SessionSocketMessage,
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
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
  externalActivities?: StateSnapshotExternalActivity[];
  externalMessages?: StateSnapshotExternalMessage[];
  focusedRunId?: string | null;
  hitlTickets?: HITLTicket[];
  hitlLoading?: boolean;
  canStop?: boolean;
  canAddHint?: boolean;
  debugOpen?: boolean;
  debugEvents?: SessionSocketMessage[];
  onCreateSession?: () => void;
  onSelectSession?: (sessionId: string) => void;
  onSearchChange?: (value: string) => void;
  onFilterChange?: (filter: SessionFilter) => void;
  onSubmitMessage?: (message: string) => void;
  onFocusRun?: (runId: string) => void;
  onRefreshHITL?: () => void;
  onRespondHITL?: (ticketId: string, instruction: string) => Promise<void> | void;
  onDismissHITL?: (ticketId: string) => Promise<void> | void;
  onStop?: () => void;
  onAddHint?: () => void;
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
  externalActivities = [],
  externalMessages = [],
  focusedRunId = null,
  hitlTickets = [],
  hitlLoading = false,
  canStop = false,
  canAddHint = false,
  debugOpen = false,
  debugEvents = [],
  onCreateSession = () => {},
  onSelectSession = () => {},
  onSearchChange = () => {},
  onFilterChange = () => {},
  onSubmitMessage = () => {},
  onFocusRun = () => {},
  onRefreshHITL = () => {},
  onRespondHITL = () => {},
  onDismissHITL = () => {},
  onStop = () => {},
  onAddHint = () => {},
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
          <div className="app-shell__stack">
            <RunInspector
              currentTurn={currentTurn}
              runs={runs}
              externalTasks={externalTasks}
              externalActivities={externalActivities}
              externalMessages={externalMessages}
              focusedRunId={focusedRunId}
              onFocusRun={onFocusRun}
              canStop={canStop}
              onStop={onStop}
              canAddHint={canAddHint}
              onAddHint={onAddHint}
              onOpenDebug={onOpenDebug}
            />
            <HITLPanel
              tickets={hitlTickets}
              loading={hitlLoading}
              onRefresh={onRefreshHITL}
              onRespond={onRespondHITL}
              onDismiss={onDismissHITL}
            />
          </div>
        </section>
      </div>
      <DebugDrawer open={debugOpen} events={debugEvents} onClose={onCloseDebug} />
    </>
  );
}
