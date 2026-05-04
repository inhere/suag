import { ConversationPane } from "../features/conversation/conversation-pane";
import type { ConversationTimelineEntry } from "../features/conversation/conversation-timeline";
import { RunDetailsCompactPanel } from "../features/context/run-details-compact-panel";
import { SessionSummaryPanel } from "../features/context/session-summary-panel";
import { DebugDrawer } from "../features/debug/debug-drawer";
import { HITLPanel } from "../features/hitl/hitl-panel";
import { MemoryCandidatesPanel } from "../features/memory-candidates/memory-candidates-panel";
import { SessionList } from "../features/session-list/session-list";
import type { SessionFilter, SessionListItem } from "../features/session/session-store";
import type {
  HITLTicket,
  MemoryCandidate,
  SessionSocketMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
} from "../lib/host-types";
import type { SessionSocketStatus } from "../lib/session-ws";
import type { InlineStatusCardState } from "../features/conversation/inline-status-card";
import type { SummaryStripTone } from "../features/conversation/summary-strip";

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
  messages?: ConversationTimelineEntry[];
  currentTurn?: StateSnapshotTurn | null;
  waitingForInput?: boolean;
  chatHeaderTitle?: string;
  chatHeaderMeta?: string | null;
  summaryStripStatus?: string;
  summaryStripTone?: SummaryStripTone;
  inlineStatus?: {
    state: InlineStatusCardState;
    detail?: string | null;
  } | null;
  submitPending?: boolean;
  submitError?: string | null;
  runs?: StateSnapshotRun[];
  focusedRunId?: string | null;
  hitlTickets?: HITLTicket[];
  hitlLoading?: boolean;
  memoryCandidates?: MemoryCandidate[];
  memoryCandidatesLoading?: boolean;
  memoryCandidatesError?: string | null;
  memoryCandidateActionError?: string | null;
  memoryCandidateActionPendingIds?: string[];
  hitlError?: string | null;
  debugOpen?: boolean;
  debugEvents?: SessionSocketMessage[];
  onCreateSession?: () => void;
  onSelectSession?: (sessionId: string) => void;
  onSearchChange?: (value: string) => void;
  onFilterChange?: (filter: SessionFilter) => void;
  onSubmitMessage?: (message: string) => void;
  onRetryMemoryCandidates?: () => void;
  onAcceptMemoryCandidate?: (candidateId: string) => void;
  onRejectMemoryCandidate?: (candidateId: string) => void;
  onRefreshHITL?: () => void;
  onOpenHITLContext?: (ticket: HITLTicket) => void;
  onRespondHITL?: (ticketId: string, instruction: string) => Promise<void> | void;
  onDismissHITL?: (ticketId: string) => Promise<void> | void;
  onCloseDebug?: () => void;
};

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
  chatHeaderTitle = "Chat",
  chatHeaderMeta = null,
  summaryStripStatus = "尚未开始，发送一条消息创建新的 turn。",
  summaryStripTone = "attention",
  inlineStatus = null,
  submitPending = false,
  submitError = null,
  runs = [],
  focusedRunId = null,
  hitlTickets = [],
  hitlLoading = false,
  memoryCandidates = [],
  memoryCandidatesLoading = false,
  memoryCandidatesError = null,
  memoryCandidateActionError = null,
  memoryCandidateActionPendingIds = [],
  hitlError = null,
  debugOpen = false,
  debugEvents = [],
  onCreateSession = () => {},
  onSelectSession = () => {},
  onSearchChange = () => {},
  onFilterChange = () => {},
  onSubmitMessage = () => {},
  onRetryMemoryCandidates = () => {},
  onAcceptMemoryCandidate = () => {},
  onRejectMemoryCandidate = () => {},
  onRefreshHITL = () => {},
  onOpenHITLContext = () => {},
  onRespondHITL = () => {},
  onDismissHITL = () => {},
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
              waitingForInput={waitingForInput}
              headerTitle={chatHeaderTitle}
              headerMeta={chatHeaderMeta}
              summaryStatus={summaryStripStatus}
              summaryTone={summaryStripTone}
              inlineStatus={inlineStatus}
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
              <SessionSummaryPanel currentTurn={currentTurn} />
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
              <RunDetailsCompactPanel runs={runs} focusedRunId={focusedRunId} />
            </div>
          </section>
        </div>
      </div>
      <DebugDrawer open={debugOpen} events={debugEvents} onClose={onCloseDebug} />
    </>
  );
}
