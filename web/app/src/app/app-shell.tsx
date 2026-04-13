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
import type { SessionSocketStatus } from "../lib/session-ws";

type AppShellProps = {
  sessions?: SessionListItem[];
  currentSessionId?: string | null;
  contextSessionLabel?: string;
  contextTurnLabel?: string;
  contextRunLabel?: string;
  contextConnectionLabel?: string;
  contextConnectionTone?: "idle" | "connecting" | "connected" | "disconnected";
  connectionStatus?: SessionSocketStatus | "idle";
  connectionStatusMessage?: string;
  connectionError?: string | null;
  onRetryConnection?: () => void;
  filter?: SessionFilter;
  searchQuery?: string;
  messages?: ConversationMessage[];
  currentTurn?: StateSnapshotTurn | null;
  waitingForInput?: boolean;
  runs?: StateSnapshotRun[];
  externalTasks?: ExternalTaskSummary[];
  externalActivities?: StateSnapshotExternalActivity[];
  externalMessages?: StateSnapshotExternalMessage[];
  recentTurns?: StateSnapshotTurn[];
  focusedRunId?: string | null;
  focusedRunDetailStatus?: "idle" | "loading" | "loaded" | "error";
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
  onSelectConversationTurn?: (turn: StateSnapshotTurn) => void;
  onFocusRun?: (runId: string) => void;
  onRetryFocusedRunDetail?: () => void;
  onRefreshHITL?: () => void;
  onOpenHITLContext?: (ticket: HITLTicket) => void;
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
  contextSessionLabel = "Session none",
  contextTurnLabel = "Turn none",
  contextRunLabel = "Run none",
  contextConnectionLabel = "Socket idle",
  contextConnectionTone = "idle",
  connectionStatus = "idle",
  connectionStatusMessage = "No active session selected.",
  connectionError = null,
  onRetryConnection = () => {},
  filter = "all",
  searchQuery = "",
  messages = [],
  currentTurn = null,
  waitingForInput = false,
  runs = [],
  externalTasks = [],
  externalActivities = [],
  externalMessages = [],
  recentTurns = [],
  focusedRunId = null,
  focusedRunDetailStatus = "idle",
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
  onSelectConversationTurn = () => {},
  onFocusRun = () => {},
  onRetryFocusedRunDetail = () => {},
  onRefreshHITL = () => {},
  onOpenHITLContext = () => {},
  onRespondHITL = () => {},
  onDismissHITL = () => {},
  onStop = () => {},
  onAddHint = () => {},
  onOpenDebug = () => {},
  onCloseDebug = () => {},
}: AppShellProps) {

  return (
    <>
      <div className="app-frame">
        <header className="app-context-bar" aria-label="Workbench context">
          <span className="app-context-bar__item">{contextSessionLabel}</span>
          <span className="app-context-bar__item">{contextTurnLabel}</span>
          <span className="app-context-bar__item">{contextRunLabel}</span>
          <span
            className={`app-context-bar__item app-context-bar__item--status app-context-bar__item--${contextConnectionTone}`}
          >
            {contextConnectionLabel}
          </span>
        </header>
        <div className="app-shell">
          <aside className="app-shell__panel" aria-label="Sessions panel">
            <SessionList
              sessions={sessions}
              currentSessionId={currentSessionId}
              connectionStatus={connectionStatus}
              statusMessage={connectionStatusMessage}
              errorMessage={connectionError}
              onRetry={onRetryConnection}
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
              recentTurns={recentTurns}
              onSelectTurn={onSelectConversationTurn}
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
                focusedRunDetailStatus={focusedRunDetailStatus}
                onFocusRun={onFocusRun}
                onRetryFocusedRunDetail={onRetryFocusedRunDetail}
                canStop={canStop}
                onStop={onStop}
                canAddHint={canAddHint}
                onAddHint={onAddHint}
                onOpenDebug={onOpenDebug}
              />
              <HITLPanel
                tickets={hitlTickets}
                loading={hitlLoading}
                currentSessionId={currentSessionId}
                onRefresh={onRefreshHITL}
                onOpenContext={onOpenHITLContext}
                onRespond={onRespondHITL}
                onDismiss={onDismissHITL}
              />
            </div>
          </section>
        </div>
      </div>
      <DebugDrawer open={debugOpen} events={debugEvents} onClose={onCloseDebug} />
    </>
  );
}
