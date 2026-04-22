import type { ConversationMessage } from "../features/conversation/conversation-pane";
import { ConversationPane } from "../features/conversation/conversation-pane";
import { DebugDrawer } from "../features/debug/debug-drawer";
import { HITLPanel } from "../features/hitl/hitl-panel";
import { MemoryCandidatesPanel } from "../features/memory-candidates/memory-candidates-panel";
import { SessionList } from "../features/session-list/session-list";
import type { SessionFilter, SessionListItem } from "../features/session/session-store";
import type {
  ExternalTaskSummary,
  HITLTicket,
  MemoryCandidate,
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
  createSessionPending?: boolean;
  createSessionError?: string | null;
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
  waitingForApproval?: boolean;
  submitPending?: boolean;
  submitError?: string | null;
  runs?: StateSnapshotRun[];
  externalTasks?: ExternalTaskSummary[];
  externalActivities?: StateSnapshotExternalActivity[];
  externalMessages?: StateSnapshotExternalMessage[];
  focusedRunId?: string | null;
  focusedRunDetailStatus?: "idle" | "loading" | "loaded" | "error";
  hitlTickets?: HITLTicket[];
  hitlLoading?: boolean;
  memoryCandidates?: MemoryCandidate[];
  memoryCandidatesLoading?: boolean;
  memoryCandidatesError?: string | null;
  memoryCandidateActionError?: string | null;
  memoryCandidateActionPendingIds?: string[];
  canStop?: boolean;
  stopPending?: boolean;
  canAddHint?: boolean;
  addHintPending?: boolean;
  runActionError?: string | null;
  hitlError?: string | null;
  debugOpen?: boolean;
  debugEvents?: SessionSocketMessage[];
  onCreateSession?: () => void;
  onSelectSession?: (sessionId: string) => void;
  onSearchChange?: (value: string) => void;
  onFilterChange?: (filter: SessionFilter) => void;
  onSubmitMessage?: (message: string) => void;
  onFocusRun?: (runId: string) => void;
  onRetryFocusedRunDetail?: () => void;
  onRetryMemoryCandidates?: () => void;
  onAcceptMemoryCandidate?: (candidateId: string) => void;
  onRejectMemoryCandidate?: (candidateId: string) => void;
  onRefreshHITL?: () => void;
  onOpenHITLContext?: (ticket: HITLTicket) => void;
  onRespondHITL?: (ticketId: string, instruction: string) => Promise<void> | void;
  onDismissHITL?: (ticketId: string) => Promise<void> | void;
  onStop?: () => void;
  onAddHint?: () => void;
  onOpenDebug?: () => void;
  onCloseDebug?: () => void;
};

// NOTE: run/external-task props are intentionally retained in Task 1.
// Task 3 will replace the right-column run details placeholder and consume them.
export function AppShell({
  sessions = [],
  currentSessionId = null,
  createSessionPending = false,
  createSessionError = null,
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
  waitingForApproval = false,
  submitPending = false,
  submitError = null,
  runs = [],
  externalTasks = [],
  externalActivities = [],
  externalMessages = [],
  focusedRunId = null,
  focusedRunDetailStatus = "idle",
  hitlTickets = [],
  hitlLoading = false,
  memoryCandidates = [],
  memoryCandidatesLoading = false,
  memoryCandidatesError = null,
  memoryCandidateActionError = null,
  memoryCandidateActionPendingIds = [],
  canStop = false,
  stopPending = false,
  canAddHint = false,
  addHintPending = false,
  runActionError = null,
  hitlError = null,
  debugOpen = false,
  debugEvents = [],
  onCreateSession = () => {},
  onSelectSession = () => {},
  onSearchChange = () => {},
  onFilterChange = () => {},
  onSubmitMessage = () => {},
  onFocusRun = () => {},
  onRetryFocusedRunDetail = () => {},
  onRetryMemoryCandidates = () => {},
  onAcceptMemoryCandidate = () => {},
  onRejectMemoryCandidate = () => {},
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
          <aside className="app-shell__sidebar" aria-label="Sessions panel">
            <SessionList
              sessions={sessions}
              currentSessionId={currentSessionId}
              createPending={createSessionPending}
              actionError={createSessionError}
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
          <main className="app-shell__main" aria-label="Chat panel">
            <ConversationPane
              messages={messages}
              currentTurn={currentTurn}
              waitingForInput={waitingForInput}
              submitPending={submitPending}
              submitError={submitError}
              onSubmit={onSubmitMessage}
            />
          </main>
          <section className="app-shell__context" aria-label="Context panel">
            <div className="app-shell__stack">
              <HITLPanel
                tickets={hitlTickets}
                loading={hitlLoading}
                currentSessionId={currentSessionId}
                errorMessage={hitlError}
                onRefresh={onRefreshHITL}
                onOpenContext={onOpenHITLContext}
                onRespond={onRespondHITL}
                onDismiss={onDismissHITL}
              />
              <section className="run-panel app-shell__context-placeholder" aria-label="Session summary panel">
                <h2>Session summary</h2>
                <p className="inline-feedback">Session summary will appear here.</p>
              </section>
              <MemoryCandidatesPanel
                candidates={memoryCandidates}
                loading={memoryCandidatesLoading}
                errorMessage={memoryCandidatesError}
                actionError={memoryCandidateActionError}
                pendingCandidateIds={memoryCandidateActionPendingIds}
                onRetry={onRetryMemoryCandidates}
                onAccept={onAcceptMemoryCandidate}
                onReject={onRejectMemoryCandidate}
              />
              <section className="run-panel app-shell__context-placeholder" aria-label="Run details panel">
                <h2>Run details</h2>
                <p className="inline-feedback">Compact run details will appear here (runs wired for next task: {runs.length}).</p>
              </section>
            </div>
          </section>
        </div>
      </div>
      <DebugDrawer open={debugOpen} events={debugEvents} onClose={onCloseDebug} />
    </>
  );
}
