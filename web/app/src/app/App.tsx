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
  selectRecentTurns,
} from "../features/session/session-selectors";
import type { HITLTicket, SessionSocketMessage, StateSnapshotTurn } from "../lib/host-types";
import {
  createSession,
  dismissHITLTicket,
  getRunDetail,
  getSession,
  listSessions,
  listHITLTickets,
  respondHITLTicket,
  startTurn,
} from "../lib/session-client";
import { connectSessionSocket, type SessionSocketConnection } from "../lib/session-ws";
import type { SessionSocketStatus } from "../lib/session-ws";

const workbenchStorageKey = "axon.web.workbench";
const socketReconnectDelayMs = 1500;

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
      timestamp: formatConversationTimestamp(currentTurn.updatedAt || snapshot.generatedAt),
    });
  }

  const focusedRun = selectFocusedRun(snapshot, state.focusedRunId);
  const focusedExternalMessage =
    snapshot.externalMessages.find((message) => message.runId === focusedRun?.runId) ?? null;
  const assistantText =
    focusedRun?.summary ||
    focusedExternalMessage?.content ||
    focusedRun?.latestMessageText ||
    "";

  if (assistantText) {
    messages.push({
      id: `${focusedRun?.runId ?? snapshot.sessionId}:assistant`,
      role: "assistant",
      text: assistantText,
      timestamp: formatConversationTimestamp(
        focusedExternalMessage?.updatedAt || focusedRun?.updatedAt || snapshot.generatedAt,
      ),
    });
  }

  return messages;
}

function formatConversationTimestamp(value: string | undefined): string {
  if (!value) {
    return "";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return "";
  }

  return new Intl.DateTimeFormat("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(parsed);
}

function isWaitingForInput(state: SessionViewState): boolean {
  const currentTurn = selectCurrentTurn(state.snapshot);
  if (!currentTurn) {
    return false;
  }

  const displayState = currentTurn.displayState.toLowerCase();
  return displayState === "input_required" || displayState === "waiting_input";
}

function isWaitingForApproval(state: SessionViewState): boolean {
  const currentTurn = selectCurrentTurn(state.snapshot);
  if (!currentTurn) {
    return false;
  }

  return currentTurn.displayState.toLowerCase() === "auth_required";
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

function buildRecentSessionPlaceholder(
  sessions: SessionListItem[],
  sessionId: string,
  fallback?: Partial<Omit<SessionListItem, "sessionId">>,
): SessionListItem {
  const existing = sessions.find((item) => item.sessionId === sessionId);
  return {
    sessionId,
    title: fallback?.title ?? existing?.title ?? `Session ${sessionId}`,
    status: fallback?.status ?? existing?.status ?? "OPEN",
    updatedAt: fallback?.updatedAt ?? new Date().toISOString(),
    preview: fallback?.preview ?? existing?.preview ?? "",
  };
}

function mergeRecentSessions(
  currentSessions: SessionListItem[],
  incomingSessions: SessionListItem[],
): SessionListItem[] {
  const merged = [...currentSessions];
  for (const session of incomingSessions) {
    const existingIndex = merged.findIndex((item) => item.sessionId === session.sessionId);
    if (existingIndex >= 0) {
      merged[existingIndex] = {
        ...merged[existingIndex],
        ...session,
      };
      continue;
    }
    merged.push(session);
  }

  return merged.sort((left, right) => right.updatedAt.localeCompare(left.updatedAt));
}

function switchSessionContext(
  state: SessionViewState,
  sessionId: string,
  options?: {
    focusedRunId?: string | null;
    fallbackTitle?: string;
    fallbackStatus?: string;
  },
): SessionViewState {
  const sessionChanged = state.currentSessionId !== sessionId;
  const hasFocusedRunOverride = !!options && "focusedRunId" in options;

  return {
    ...state,
    currentSessionId: sessionId,
    focusedRunId: hasFocusedRunOverride
      ? options?.focusedRunId ?? null
      : sessionChanged
        ? null
        : state.focusedRunId,
    // Clear stale cross-session data before the new bootstrap / snapshot arrives.
    snapshot: sessionChanged ? null : state.snapshot,
    runDetailStatus: sessionChanged ? {} : state.runDetailStatus,
    recentSessions: [
      buildRecentSessionPlaceholder(state.recentSessions, sessionId, {
        title: options?.fallbackTitle,
        status: options?.fallbackStatus,
      }),
      ...state.recentSessions.filter((item) => item.sessionId !== sessionId),
    ],
  };
}

export function App() {
  const [viewState, setViewState] = useState<SessionViewState>(buildInitialViewState);
  const [hitlTickets, setHITLTickets] = useState<HITLTicket[]>([]);
  const [hitlLoading, setHITLLoading] = useState(false);
  const [createSessionPending, setCreateSessionPending] = useState(false);
  const [createSessionError, setCreateSessionError] = useState<string | null>(null);
  const [startTurnPending, setStartTurnPending] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [pendingWSCommand, setPendingWSCommand] = useState<
    null | "continue_composer" | "continue_hint" | "stop"
  >(null);
  const [runActionError, setRunActionError] = useState<string | null>(null);
  const [hitlError, setHITLError] = useState<string | null>(null);
  const [socketStatus, setSocketStatus] = useState<SessionSocketStatus | "idle">("idle");
  const [connectionError, setConnectionError] = useState<string | null>(null);
  const [sessionListReloadKey, setSessionListReloadKey] = useState(0);
  const [connectionReloadKey, setConnectionReloadKey] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [filter, setFilter] = useState<SessionFilter>("all");
  const [debugOpen, setDebugOpen] = useState(false);
  const socketRef = useRef<SessionSocketConnection | null>(null);
  const pendingWSCommandRef = useRef<null | "continue_composer" | "continue_hint" | "stop">(null);
  const lastWSCommandRef = useRef<null | "continue_composer" | "continue_hint" | "stop">(null);
  const runDetailRequestIDRef = useRef(0);

  useEffect(() => {
    persistWorkbenchState(viewState);
  }, [viewState]);

  useEffect(() => {
    pendingWSCommandRef.current = pendingWSCommand;
  }, [pendingWSCommand]);

  function setPendingWSCommandState(command: null | "continue_composer" | "continue_hint" | "stop") {
    pendingWSCommandRef.current = command;
    if (command) {
      lastWSCommandRef.current = command;
    }
    setPendingWSCommand(command);
  }

  function requestRunDetail(sessionId: string, runId: string) {
    const requestID = runDetailRequestIDRef.current + 1;
    runDetailRequestIDRef.current = requestID;
    setViewState((current) => markRunDetailRequested(current, runId));

    void getRunDetail(sessionId, runId)
      .then((detail) => {
        setViewState((current) => {
          if (runDetailRequestIDRef.current !== requestID) {
            return current;
          }
          if (current.currentSessionId !== sessionId || current.focusedRunId !== runId) {
            return current;
          }

          return mergeRunDetail(current, detail, new Date().toISOString());
        });
      })
      .catch(() => {
        setViewState((current) => {
          if (runDetailRequestIDRef.current !== requestID) {
            return current;
          }
          if (current.currentSessionId !== sessionId || current.focusedRunId !== runId) {
            return current;
          }

          return markRunDetailFailed(current, runId);
        });
      });
  }

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
    let cancelled = false;
    setConnectionError((current) =>
      current === "Failed to load session list." ? null : current,
    );

    void listSessions()
      .then((sessions) => {
        if (cancelled || !sessions.length) {
          return;
        }

        const mappedSessions: SessionListItem[] = sessions.map((session) => ({
          sessionId: session.sessionId,
          title: session.title,
          status: session.status,
          updatedAt: session.updatedAt,
          preview: session.preview,
        }));

        setViewState((current) => {
          const mergedSessions = mergeRecentSessions(current.recentSessions, mappedSessions);
          if (current.currentSessionId) {
            return {
              ...current,
              recentSessions: mergedSessions,
            };
          }

          return switchSessionContext(
            {
              ...current,
              recentSessions: mergedSessions,
            },
            mappedSessions[0].sessionId,
            {
              focusedRunId: null,
              fallbackTitle: mappedSessions[0].title,
              fallbackStatus: mappedSessions[0].status,
            },
          );
        });
      })
      .catch(() => {
        // Keep v0 usable even when the session list endpoint is temporarily unavailable.
        if (!cancelled) {
          setConnectionError("Failed to load session list.");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [sessionListReloadKey]);

  useEffect(() => {
    if (!viewState.currentSessionId) {
      setSocketStatus("idle");
      setConnectionError(null);
      return undefined;
    }

    let cancelled = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    setSocketStatus("connecting");
    setConnectionError(null);
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
        if (cancelled) {
          return;
        }
        // v0 keeps the shell usable even when the detail bootstrap fails temporarily.
        setConnectionError("Failed to load session details.");
      });

    socketRef.current?.close();
    socketRef.current = connectSessionSocket({
      sessionId: viewState.currentSessionId,
      onStatusChange: (status) => {
        if (cancelled) {
          return;
        }
        setSocketStatus(status);
        if (status === "disconnected") {
          reconnectTimer = setTimeout(() => {
            if (cancelled) {
              return;
            }
            setConnectionReloadKey((current) => current + 1);
          }, socketReconnectDelayMs);
        }
      },
      onError: (message) => {
        if (cancelled) {
          return;
        }
        setPendingWSCommandState(null);
        setSubmitError(null);
        setRunActionError(null);
        setConnectionError(message);
      },
      onMessage: (message: SessionSocketMessage) => {
        if (cancelled) {
          return;
        }

        if (message.type === "command.accepted") {
          setSubmitError(null);
          setRunActionError(null);
          setPendingWSCommandState(null);
          lastWSCommandRef.current = null;
        }
        if (message.type === "command.error") {
          const pendingCommand = pendingWSCommandRef.current ?? lastWSCommandRef.current;
          if (pendingCommand === "continue_composer") {
            setSubmitError("Failed to continue turn.");
          }
          if (pendingCommand === "continue_hint") {
            setRunActionError("Failed to add hint.");
          }
          if (pendingCommand === "stop") {
            setRunActionError("Failed to stop turn.");
          }
          setPendingWSCommandState(null);
          lastWSCommandRef.current = null;
        }

        setViewState((current) => reduceSessionMessage(current, message));
      },
    });

    return () => {
      cancelled = true;
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
      }
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [viewState.currentSessionId, connectionReloadKey]);

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

    requestRunDetail(sessionId, focusedRunId);
  }, [viewState.currentSessionId, viewState.focusedRunId, viewState.snapshot]);

  const snapshot = viewState.snapshot;
  const currentTurn = selectCurrentTurn(snapshot);
  const activeRuns = selectActiveRuns(snapshot);
  const focusedRun = selectFocusedRun(snapshot, viewState.focusedRunId);
  const recentTurns = selectRecentTurns(snapshot, 4);
  const conversationMessages = buildConversationMessages(viewState);
  const waitingForInput = isWaitingForInput(viewState);
  const waitingForApproval = isWaitingForApproval(viewState);
  const stopEnabled = canStopCurrentTurn(viewState);
  const submitPending = startTurnPending || pendingWSCommand === "continue_composer";
  const stopPending = pendingWSCommand === "stop";
  const addHintPending = pendingWSCommand === "continue_hint";
  const contextSessionLabel = `Session ${viewState.currentSessionId ?? "none"}`;
  const contextTurnLabel = `Turn ${currentTurn?.turnId ?? "none"}`;
  const contextRunLabel = `Run ${focusedRun?.runId ?? viewState.focusedRunId ?? "none"}`;
  const contextConnectionLabel = !viewState.currentSessionId
    ? "Socket idle"
    : socketStatus === "connected"
      ? "Socket connected"
      : socketStatus === "disconnected"
        ? "Socket disconnected"
        : "Socket connecting";
  const connectionStatusMessage = !viewState.currentSessionId
    ? "No active session selected."
    : socketStatus === "connected"
      ? `Session ${viewState.currentSessionId} connected.`
      : socketStatus === "disconnected"
        ? `Session ${viewState.currentSessionId} disconnected.`
        : `Session ${viewState.currentSessionId} connecting...`;

  async function createNewSession(): Promise<string> {
    setCreateSessionError(null);
    setCreateSessionPending(true);
    try {
      const created = await createSession({ title: "host runtime" });
      setViewState((current) =>
        switchSessionContext(current, created.sessionId, {
          focusedRunId: null,
          fallbackTitle: created.title,
          fallbackStatus: created.status,
        }),
      );
      return created.sessionId;
    } catch (error) {
      setCreateSessionError("Failed to create session.");
      throw error;
    } finally {
      setCreateSessionPending(false);
    }
  }

  async function ensureSession(): Promise<string> {
    if (viewState.currentSessionId) {
      return viewState.currentSessionId;
    }

    return createNewSession();
  }

  async function handleSubmit(message: string) {
    if (submitPending) {
      return;
    }

    setSubmitError(null);
    if (waitingForInput) {
      setRunActionError(null);
      setPendingWSCommandState("continue_composer");
      const sessionId = await ensureSession().catch(() => null);
      if (!sessionId) {
        setPendingWSCommandState(null);
        return;
      }
      socketRef.current?.sendContinueTurn({
        message,
        agentId: "default",
        initiatorType: "user",
      });
      return;
    }

    const sessionId = await ensureSession().catch(() => null);
    if (!sessionId) {
      return;
    }

    setStartTurnPending(true);
    try {
      await startTurn(sessionId, {
        agentId: "default",
        message,
        initiatorType: "user",
      });
    } catch (error) {
      setSubmitError("Failed to start turn.");
      throw error;
    } finally {
      setStartTurnPending(false);
    }
  }

  function handleAddHint() {
    if (!waitingForInput || addHintPending) {
      return;
    }

    const message = buildHintMessage();
    if (!message) {
      return;
    }

    setRunActionError(null);
    setPendingWSCommandState("continue_hint");
    socketRef.current?.sendContinueTurn({
      message,
      agentId: "default",
      initiatorType: "user",
    });
  }

  function handleStopTurn() {
    if (!stopEnabled || !currentTurn || stopPending) {
      return;
    }

    setRunActionError(null);
    setPendingWSCommandState("stop");
    socketRef.current?.sendStopTurn({
      turnId: currentTurn.turnId,
    });
  }

  async function handleRespondHITL(ticketId: string, instruction: string) {
    setHITLError(null);
    try {
      await respondHITLTicket(ticketId, instruction);
      await refreshHITLTickets();
    } catch (error) {
      setHITLError("Failed to respond to HITL ticket.");
    }
  }

  async function handleDismissHITL(ticketId: string) {
    setHITLError(null);
    try {
      await dismissHITLTicket(ticketId);
      await refreshHITLTickets();
    } catch (error) {
      setHITLError("Failed to dismiss HITL ticket.");
    }
  }

  function handleOpenHITLContext(ticket: HITLTicket) {
    const targetSessionId = ticket.sessionId || viewState.currentSessionId;
    if (!targetSessionId) {
      return;
    }

    setHITLError(null);
    setViewState((current) =>
      switchSessionContext(current, targetSessionId, {
        focusedRunId: ticket.runId || null,
      }),
    );
  }

  function handleRetryConnection() {
    if (viewState.currentSessionId) {
      setConnectionReloadKey((current) => current + 1);
      return;
    }

    setSessionListReloadKey((current) => current + 1);
  }

  function handleRetryFocusedRunDetail() {
    if (!viewState.currentSessionId || !viewState.focusedRunId) {
      return;
    }

    requestRunDetail(viewState.currentSessionId, viewState.focusedRunId);
  }

  function handleSelectConversationTurn(turn: StateSnapshotTurn) {
    setViewState((current) => ({
      ...current,
      focusedRunId: turn.latestRunId || turn.currentRunId || turn.primaryRunId || null,
    }));
  }

  return (
    <AppShell
      sessions={viewState.recentSessions}
      currentSessionId={viewState.currentSessionId}
      createSessionPending={createSessionPending}
      createSessionError={createSessionError}
      contextSessionLabel={contextSessionLabel}
      contextTurnLabel={contextTurnLabel}
      contextRunLabel={contextRunLabel}
      contextConnectionLabel={contextConnectionLabel}
      contextConnectionTone={viewState.currentSessionId ? socketStatus : "idle"}
      connectionStatus={socketStatus}
      connectionStatusMessage={connectionStatusMessage}
      connectionError={connectionError}
      onRetryConnection={handleRetryConnection}
      filter={filter}
      searchQuery={searchQuery}
      messages={conversationMessages}
      currentTurn={currentTurn}
      recentTurns={recentTurns}
      waitingForInput={waitingForInput}
      submitPending={submitPending}
      submitError={submitError}
      runs={snapshot?.runs.length ? snapshot.runs : activeRuns}
      externalTasks={snapshot?.externalTasks ?? []}
      externalActivities={snapshot?.externalActivities ?? []}
      externalMessages={snapshot?.externalMessages ?? []}
      focusedRunId={focusedRun?.runId ?? viewState.focusedRunId}
      focusedRunDetailStatus={
        focusedRun ? viewState.runDetailStatus[focusedRun.runId] ?? "idle" : "idle"
      }
      hitlTickets={hitlTickets}
      hitlLoading={hitlLoading}
      canStop={stopEnabled}
      stopPending={stopPending}
      canAddHint={waitingForInput}
      waitingForApproval={waitingForApproval}
      addHintPending={addHintPending}
      runActionError={runActionError}
      hitlError={hitlError}
      debugOpen={debugOpen}
      debugEvents={snapshot?.events ?? []}
      onCreateSession={() => {
        void createNewSession().catch(() => {});
      }}
      onSelectSession={(sessionId) => {
        setViewState((current) =>
          switchSessionContext(current, sessionId, {
            focusedRunId: null,
          }),
        );
      }}
      onSearchChange={setSearchQuery}
      onFilterChange={setFilter}
      onSubmitMessage={(message) => {
        void handleSubmit(message).catch(() => {});
      }}
      onSelectConversationTurn={handleSelectConversationTurn}
      onFocusRun={(runId) => {
        setViewState((current) => ({
          ...current,
          focusedRunId: runId,
        }));
      }}
      onRetryFocusedRunDetail={handleRetryFocusedRunDetail}
      onRefreshHITL={() => {
        void refreshHITLTickets();
      }}
      onOpenHITLContext={handleOpenHITLContext}
      onRespondHITL={(ticketId, instruction) => handleRespondHITL(ticketId, instruction)}
      onDismissHITL={(ticketId) => handleDismissHITL(ticketId)}
      onStop={handleStopTurn}
      onAddHint={handleAddHint}
      onOpenDebug={() => setDebugOpen(true)}
      onCloseDebug={() => setDebugOpen(false)}
    />
  );
}
