import { useEffect, useRef, useState } from "react";
import { AppShell } from "./app-shell";
import type { ConversationMessage } from "../features/conversation/conversation-pane";
import {
  emptySessionViewState,
  markRunDetailFailed,
  markRunDetailRequested,
  mergeRunDetail,
  mergeSessionDetail,
  shouldLoadRunDetail,
  type SessionFilter,
  type SessionListItem,
  type SessionViewState,
  reduceSessionMessage,
} from "../features/session/session-store";
import {
  selectActiveRuns,
  selectCurrentTurn,
  selectFocusedRun,
} from "../features/session/session-selectors";
import type { SessionSocketMessage } from "../lib/host-types";
import {
  createSession,
  dismissHITLTicket,
  getRunDetail,
  getSession,
  listHITLTickets,
  respondHITLTicket,
  startTurn,
} from "../lib/session-client";
import { connectSessionSocket, type SessionSocketConnection } from "../lib/session-ws";

const workbenchStorageKey = "axon.web.workbench";

type PersistedWorkbenchState = {
  recentSessions: SessionListItem[];
  currentSessionId: string | null;
  focusedRunId: string | null;
};

function buildConversationMessages(state: SessionViewState): ConversationMessage[] {
  const messages: ConversationMessage[] = [];
  const snapshot = state.snapshot;
  if (!snapshot) {
    return messages;
  }

  const currentTurn = selectCurrentTurn(snapshot);
  if (currentTurn?.latestMessagePreview) {
    messages.push({
      id: `${currentTurn.turnId}:user`,
      role: "user",
      text: currentTurn.latestMessagePreview,
    });
  }

  const focusedRun = selectFocusedRun(snapshot, state.focusedRunId);
  const assistantText =
    focusedRun?.summary ||
    snapshot.externalMessages.find((message) => message.runId === focusedRun?.runId)?.content ||
    focusedRun?.latestMessageText ||
    "";

  if (assistantText) {
    messages.push({
      id: `${focusedRun?.runId ?? snapshot.sessionId}:assistant`,
      role: "assistant",
      text: assistantText,
    });
  }

  return messages;
}

function isWaitingForInput(state: SessionViewState): boolean {
  const currentTurn = selectCurrentTurn(state.snapshot);
  if (!currentTurn) {
    return false;
  }

  return currentTurn.displayState.toLowerCase().includes("waiting");
}

function canStopCurrentTurn(state: SessionViewState): boolean {
  const currentTurn = selectCurrentTurn(state.snapshot);
  if (!currentTurn) {
    return false;
  }

  const displayState = currentTurn.displayState.toLowerCase();
  return !["completed", "failed", "canceled", "waiting", "waiting_input"].includes(displayState);
}

function buildHintMessage(): string | null {
  const nextHint = globalThis.prompt?.("Add a hint for the current turn");
  if (!nextHint) {
    return null;
  }

  const trimmed = nextHint.trim();
  return trimmed ? trimmed : null;
}

function loadPersistedWorkbenchState(): PersistedWorkbenchState | null {
  try {
    const raw = globalThis.localStorage?.getItem(workbenchStorageKey);
    if (!raw) {
      return null;
    }

    const parsed = JSON.parse(raw) as Partial<PersistedWorkbenchState>;
    return {
      recentSessions: Array.isArray(parsed.recentSessions) ? parsed.recentSessions : [],
      currentSessionId:
        typeof parsed.currentSessionId === "string" ? parsed.currentSessionId : null,
      focusedRunId: typeof parsed.focusedRunId === "string" ? parsed.focusedRunId : null,
    };
  } catch {
    return null;
  }
}

function buildInitialViewState(): SessionViewState {
  const persisted = loadPersistedWorkbenchState();
  if (!persisted) {
    return emptySessionViewState;
  }

  return {
    ...emptySessionViewState,
    recentSessions: persisted.recentSessions,
    currentSessionId: persisted.currentSessionId,
    focusedRunId: persisted.focusedRunId,
  };
}

function persistWorkbenchState(state: SessionViewState) {
  try {
    globalThis.localStorage?.setItem(
      workbenchStorageKey,
      JSON.stringify({
        recentSessions: state.recentSessions,
        currentSessionId: state.currentSessionId,
        focusedRunId: state.focusedRunId,
      } satisfies PersistedWorkbenchState),
    );
  } catch {
    // Ignore persistence failures in v0 so the workbench stays usable.
  }
}

export function App() {
  const [viewState, setViewState] = useState<SessionViewState>(buildInitialViewState);
  const [hitlTickets, setHITLTickets] = useState<Array<{
    id: string;
    agentId: string;
    taskId: string;
    reason: string;
    context: string;
    status: string;
    createdAt: string;
  }>>([]);
  const [hitlLoading, setHITLLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [filter, setFilter] = useState<SessionFilter>("all");
  const [debugOpen, setDebugOpen] = useState(false);
  const socketRef = useRef<SessionSocketConnection | null>(null);

  useEffect(() => {
    persistWorkbenchState(viewState);
  }, [viewState]);

  async function refreshHITLTickets() {
    setHITLLoading(true);
    try {
      setHITLTickets(await listHITLTickets());
    } catch {
      // Keep the workbench usable even if HITL listing is temporarily unavailable.
    } finally {
      setHITLLoading(false);
    }
  }

  useEffect(() => {
    void refreshHITLTickets();
  }, []);

  useEffect(() => {
    if (!viewState.currentSessionId) {
      return undefined;
    }

    let cancelled = false;
    void getSession(viewState.currentSessionId)
      .then((detail) => {
        if (cancelled) {
          return;
        }

        setViewState((current) =>
          mergeSessionDetail(current, detail, new Date().toISOString()),
        );
      })
      .catch(() => {
        // v0 keeps the shell usable even when the detail bootstrap fails temporarily.
      });

    socketRef.current?.close();
    socketRef.current = connectSessionSocket({
      sessionId: viewState.currentSessionId,
      onMessage: (message: SessionSocketMessage) => {
        if (cancelled) {
          return;
        }

        setViewState((current) => reduceSessionMessage(current, message));
      },
    });

    return () => {
      cancelled = true;
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [viewState.currentSessionId]);

  useEffect(() => {
    const sessionId = viewState.currentSessionId;
    const focusedRunId = viewState.focusedRunId;
    const snapshot = viewState.snapshot;
    if (!sessionId || !focusedRunId || !snapshot) {
      return;
    }

    if (!shouldLoadRunDetail(viewState, focusedRunId)) {
      return;
    }

    let cancelled = false;
    setViewState((current) => markRunDetailRequested(current, focusedRunId));

    void getRunDetail(sessionId, focusedRunId)
      .then((detail) => {
        if (cancelled) {
          return;
        }

        setViewState((current) =>
          mergeRunDetail(current, detail, new Date().toISOString()),
        );
      })
      .catch(() => {
        if (cancelled) {
          return;
        }

        setViewState((current) => markRunDetailFailed(current, focusedRunId));
      });

    return () => {
      cancelled = true;
    };
  }, [viewState.currentSessionId, viewState.focusedRunId, viewState.snapshot]);

  const snapshot = viewState.snapshot;
  const currentTurn = selectCurrentTurn(snapshot);
  const activeRuns = selectActiveRuns(snapshot);
  const focusedRun = selectFocusedRun(snapshot, viewState.focusedRunId);
  const conversationMessages = buildConversationMessages(viewState);
  const waitingForInput = isWaitingForInput(viewState);
  const stopEnabled = canStopCurrentTurn(viewState);

  async function createNewSession(): Promise<string> {
    const created = await createSession({ title: "host runtime" });
    setViewState((current) => ({
      ...current,
      currentSessionId: created.sessionId,
      recentSessions: [
        {
          sessionId: created.sessionId,
          title: created.title,
          status: created.status,
          updatedAt: new Date().toISOString(),
          preview: "",
        },
        ...current.recentSessions.filter((item) => item.sessionId !== created.sessionId),
      ],
    }));
    return created.sessionId;
  }

  async function ensureSession(): Promise<string> {
    if (viewState.currentSessionId) {
      return viewState.currentSessionId;
    }

    return createNewSession();
  }

  async function handleSubmit(message: string) {
    const sessionId = await ensureSession();

    if (waitingForInput) {
      socketRef.current?.sendContinueTurn({
        message,
        agentId: "default",
        initiatorType: "user",
      });
      return;
    }

    await startTurn(sessionId, {
      agentId: "default",
      message,
      initiatorType: "user",
    });
  }

  function handleAddHint() {
    if (!waitingForInput) {
      return;
    }

    const message = buildHintMessage();
    if (!message) {
      return;
    }

    socketRef.current?.sendContinueTurn({
      message,
      agentId: "default",
      initiatorType: "user",
    });
  }

  function handleStopTurn() {
    if (!stopEnabled || !currentTurn) {
      return;
    }

    socketRef.current?.sendStopTurn({
      turnId: currentTurn.turnId,
    });
  }

  async function handleRespondHITL(ticketId: string, instruction: string) {
    await respondHITLTicket(ticketId, instruction);
    await refreshHITLTickets();
  }

  async function handleDismissHITL(ticketId: string) {
    await dismissHITLTicket(ticketId);
    await refreshHITLTickets();
  }

  return (
    <AppShell
      sessions={viewState.recentSessions}
      currentSessionId={viewState.currentSessionId}
      filter={filter}
      searchQuery={searchQuery}
      messages={conversationMessages}
      currentTurn={currentTurn}
      waitingForInput={waitingForInput}
      runs={snapshot?.runs.length ? snapshot.runs : activeRuns}
      externalTasks={snapshot?.externalTasks ?? []}
      externalActivities={snapshot?.externalActivities ?? []}
      externalMessages={snapshot?.externalMessages ?? []}
      focusedRunId={focusedRun?.runId ?? viewState.focusedRunId}
      hitlTickets={hitlTickets}
      hitlLoading={hitlLoading}
      canStop={stopEnabled}
      canAddHint={waitingForInput}
      debugOpen={debugOpen}
      debugEvents={snapshot?.events ?? []}
      onCreateSession={() => {
        void createNewSession();
      }}
      onSelectSession={(sessionId) => {
        setViewState((current) => ({
          ...current,
          currentSessionId: sessionId,
        }));
      }}
      onSearchChange={setSearchQuery}
      onFilterChange={setFilter}
      onSubmitMessage={(message) => {
        void handleSubmit(message);
      }}
      onFocusRun={(runId) => {
        setViewState((current) => ({
          ...current,
          focusedRunId: runId,
        }));
      }}
      onRefreshHITL={() => {
        void refreshHITLTickets();
      }}
      onRespondHITL={(ticketId, instruction) => handleRespondHITL(ticketId, instruction)}
      onDismissHITL={(ticketId) => handleDismissHITL(ticketId)}
      onStop={handleStopTurn}
      onAddHint={handleAddHint}
      onOpenDebug={() => setDebugOpen(true)}
      onCloseDebug={() => setDebugOpen(false)}
    />
  );
}
