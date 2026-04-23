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
import type {
  HITLTicket,
  MemoryCandidate,
  SessionSocketMessage,
} from "../lib/host-types";
import {
  acceptMemoryCandidate,
  createSession,
  dismissHITLTicket,
  getRunDetail,
  getSession,
  listMemoryCandidates,
  listSessions,
  listHITLTickets,
  rejectMemoryCandidate,
  respondHITLTicket,
  startTurn,
} from "../lib/session-client";
import { connectSessionSocket, type SessionSocketConnection } from "../lib/session-ws";
import type { SessionSocketStatus } from "../lib/session-ws";
import type { InlineStatusCardState } from "../features/conversation/inline-status-card";
import type { SummaryStripTone } from "../features/conversation/summary-strip";

const workbenchStorageKey = "axon.web.workbench";
const socketReconnectDelayMs = 1500;
const hitlPollIntervalMs = 3000;

type PersistedWorkbenchState = {
  recentSessions: SessionListItem[];
  currentSessionId: string | null;
  focusedRunId: string | null;
};

type PendingOptimisticMessage = {
  sessionId: string | null;
  message: ConversationMessage;
};

const pendingMemoryCandidateStatuses = new Set(["draft", "rendered"]);

function filterPendingMemoryCandidates(candidates: MemoryCandidate[]): MemoryCandidate[] {
  return candidates.filter((candidate) =>
    pendingMemoryCandidateStatuses.has(candidate.status.toLowerCase()),
  );
}

function buildConversationMessages(
  state: SessionViewState,
  optimisticMessage?: PendingOptimisticMessage | null,
): ConversationMessage[] {
  const messages: ConversationMessage[] = [];
  const snapshot = state.snapshot;
  const snapshotUserText = selectCurrentTurn(snapshot)?.inputSummary?.trim() ?? "";

  if (
    optimisticMessage &&
    (optimisticMessage.sessionId == null || optimisticMessage.sessionId === state.currentSessionId) &&
    optimisticMessage.message.text.trim() &&
    optimisticMessage.message.text.trim() !== snapshotUserText
  ) {
    messages.push(optimisticMessage.message);
  }

  if (!snapshot) {
    return messages;
  }

  const currentTurn = selectCurrentTurn(snapshot);
  if (currentTurn?.inputSummary?.trim()) {
    messages.push({
      id: `${currentTurn.turnId}:user`,
      role: "user",
      text: currentTurn.inputSummary,
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

function createOptimisticUserMessage(text: string): ConversationMessage {
  const now = new Date().toISOString();
  return {
    id: `optimistic:${now}:${Math.random().toString(36).slice(2, 8)}`,
    role: "user",
    text,
    timestamp: formatConversationTimestamp(now),
  };
}

function isWaitingForInput(state: SessionViewState): boolean {
  const currentTurn = selectCurrentTurn(state.snapshot);
  if (!currentTurn) {
    return false;
  }

  const displayState = currentTurn.displayState.toLowerCase();
  return displayState === "input_required" || displayState === "waiting_input";
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

function normalizeState(value: string | undefined): string {
  return (value ?? "").toLowerCase();
}

function humanizeState(value: string | undefined): string {
  const normalized = normalizeState(value);
  if (!normalized) {
    return "idle";
  }

  return normalized.replaceAll("_", " ");
}

function deriveSummaryTone(displayState: string): SummaryStripTone {
  if (displayState.includes("failed") || displayState.includes("error")) {
    return "failed";
  }

  if (displayState.includes("input_required") || displayState.includes("waiting")) {
    return "waiting";
  }

  if (displayState.includes("auth_required") || displayState.includes("cancel")) {
    return "attention";
  }

  if (displayState.includes("running") || displayState.includes("dispatch")) {
    return "running";
  }

  return "attention";
}

function selectDisplayState(
  currentTurn: ReturnType<typeof selectCurrentTurn>,
  focusedRun: ReturnType<typeof selectFocusedRun>,
): string {
  const turnState = normalizeState(currentTurn?.displayState);
  if (
    turnState.includes("auth_required") ||
    turnState.includes("input_required") ||
    turnState.includes("waiting_input") ||
    turnState.includes("failed") ||
    turnState.includes("error") ||
    turnState.includes("cancel") ||
    turnState.includes("completed")
  ) {
    return turnState;
  }

  return normalizeState(focusedRun?.displayState || currentTurn?.displayState);
}

function buildSummaryStrip(
  currentTurn: ReturnType<typeof selectCurrentTurn>,
  focusedRun: ReturnType<typeof selectFocusedRun>,
): { status: string; tone: SummaryStripTone } {
  if (!currentTurn) {
    return {
      status: "尚未开始，发送一条消息创建新的 turn。",
      tone: "attention",
    };
  }

  const displayState = selectDisplayState(currentTurn, focusedRun);
  const tone = deriveSummaryTone(displayState);

  if (displayState.includes("completed")) {
    const assistantText = stringsFirstNonEmpty(
      focusedRun?.summary,
      focusedRun?.latestMessageText,
    );
    if (assistantText) {
      return {
        status: "已完成",
        tone,
      };
    }
  }

  if (currentTurn.latestMessagePreview.trim()) {
    return {
      status: currentTurn.latestMessagePreview,
      tone,
    };
  }

  const parts = [`Turn ${currentTurn.turnId}`, humanizeState(displayState)];
  if (focusedRun?.agentId) {
    parts.push(focusedRun.agentId);
  }
  if (focusedRun?.runId) {
    parts.push(focusedRun.runId);
  }

  return {
    status: parts.join(" · "),
    tone,
  };
}

function stringsFirstNonEmpty(...values: Array<string | null | undefined>): string {
  for (const value of values) {
    if (value && value.trim()) {
      return value;
    }
  }
  return "";
}

function buildInlineStatus(
  currentTurn: ReturnType<typeof selectCurrentTurn>,
  focusedRun: ReturnType<typeof selectFocusedRun>,
  messages: ConversationMessage[],
): { state: InlineStatusCardState; detail?: string | null } | null {
  if (!currentTurn) {
    return null;
  }

  const displayState = selectDisplayState(currentTurn, focusedRun);
  const detail = currentTurn.latestMessagePreview || focusedRun?.latestMessageText || null;

  if (displayState.includes("auth_required")) {
    return { state: "auth_required", detail };
  }

  if (displayState.includes("input_required") || displayState.includes("waiting_input")) {
    return { state: "input_required", detail };
  }

  if (displayState.includes("failed") || displayState.includes("error")) {
    return { state: "failed", detail };
  }

  if (displayState.includes("cancel")) {
    return { state: "canceled", detail };
  }

  const hasAssistantMessage = messages.some((message) => message.role === "assistant" && message.text.trim());
  if (displayState.includes("completed") && !hasAssistantMessage) {
    return {
      state: "completed_empty",
      detail: detail || "当前回合已完成，但还没有新的 assistant 文本。",
    };
  }

  return null;
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
    preserveRecentSessionOrder?: boolean;
  },
): SessionViewState {
  const sessionChanged = state.currentSessionId !== sessionId;
  const hasFocusedRunOverride = !!options && "focusedRunId" in options;
  const nextRecentSessions = options?.preserveRecentSessionOrder
    ? state.recentSessions.some((item) => item.sessionId === sessionId)
      ? state.recentSessions
      : [...state.recentSessions, buildRecentSessionPlaceholder(state.recentSessions, sessionId, {
          title: options?.fallbackTitle,
          status: options?.fallbackStatus,
        })]
    : [
        buildRecentSessionPlaceholder(state.recentSessions, sessionId, {
          title: options?.fallbackTitle,
          status: options?.fallbackStatus,
        }),
        ...state.recentSessions.filter((item) => item.sessionId !== sessionId),
      ];

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
    recentSessions: nextRecentSessions,
  };
}

export function App() {
  const [viewState, setViewState] = useState<SessionViewState>(buildInitialViewState);
  const [pendingOptimisticMessage, setPendingOptimisticMessage] =
    useState<PendingOptimisticMessage | null>(null);
  const [hitlTickets, setHITLTickets] = useState<HITLTicket[]>([]);
  const [memoryCandidates, setMemoryCandidates] = useState<MemoryCandidate[]>([]);
  const [memoryCandidatesLoading, setMemoryCandidatesLoading] = useState(false);
  const [memoryCandidatesError, setMemoryCandidatesError] = useState<string | null>(null);
  const [memoryCandidateActionPendingIds, setMemoryCandidateActionPendingIDs] = useState<string[]>(
    [],
  );
  const [memoryCandidateActionError, setMemoryCandidateActionError] = useState<string | null>(null);
  const [hitlLoading, setHITLLoading] = useState(false);
  const [createSessionPending, setCreateSessionPending] = useState(false);
  const [createSessionError, setCreateSessionError] = useState<string | null>(null);
  const [startTurnPending, setStartTurnPending] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [pendingWSCommand, setPendingWSCommand] = useState<null | "continue_composer">(null);
  const [hitlError, setHITLError] = useState<string | null>(null);
  const [socketStatus, setSocketStatus] = useState<SessionSocketStatus | "idle">("idle");
  const [connectionError, setConnectionError] = useState<string | null>(null);
  const [sessionListReloadKey, setSessionListReloadKey] = useState(0);
  const [connectionReloadKey, setConnectionReloadKey] = useState(0);
  const [searchQuery, setSearchQuery] = useState("");
  const [filter, setFilter] = useState<SessionFilter>("all");
  const [debugOpen, setDebugOpen] = useState(false);
  const socketRef = useRef<SessionSocketConnection | null>(null);
  const currentSessionIDRef = useRef<string | null>(viewState.currentSessionId);
  const memoryCandidatesRefreshRequestIDRef = useRef(0);
  const memoryCandidateActionPendingIDsRef = useRef<Set<string>>(new Set());
  const pendingWSCommandRef = useRef<null | "continue_composer">(null);
  const lastWSCommandRef = useRef<null | "continue_composer">(null);
  const runDetailRequestIDRef = useRef(0);

  useEffect(() => {
    persistWorkbenchState(viewState);
  }, [viewState]);

  useEffect(() => {
    pendingWSCommandRef.current = pendingWSCommand;
  }, [pendingWSCommand]);

  useEffect(() => {
    currentSessionIDRef.current = viewState.currentSessionId;
  }, [viewState.currentSessionId]);

  useEffect(() => {
    const sessionId = viewState.currentSessionId;
    const snapshotUserText = selectCurrentTurn(viewState.snapshot)?.inputSummary?.trim() ?? "";
    if (!snapshotUserText) {
      return;
    }

    setPendingOptimisticMessage((current) => {
      if (!current || current.message.text.trim() !== snapshotUserText) {
        return current;
      }
      if (current.sessionId != null && current.sessionId !== sessionId) {
        return current;
      }
      return null;
    });
  }, [viewState.currentSessionId, viewState.snapshot]);

  function setPendingWSCommandState(command: null | "continue_composer") {
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

  async function refreshHITLTickets(showLoading = true) {
    if (showLoading) {
      setHITLLoading(true);
    }
    try {
      setHITLTickets(await listHITLTickets());
    } catch {
      // Keep the workbench usable even if HITL listing is temporarily unavailable.
    } finally {
      if (showLoading) {
        setHITLLoading(false);
      }
    }
  }

  async function refreshMemoryCandidates(sessionId: string) {
    const requestID = memoryCandidatesRefreshRequestIDRef.current + 1;
    memoryCandidatesRefreshRequestIDRef.current = requestID;
    setMemoryCandidatesLoading(true);
    setMemoryCandidatesError(null);

    try {
      const candidates = await listMemoryCandidates(sessionId);
      if (
        currentSessionIDRef.current !== sessionId ||
        memoryCandidatesRefreshRequestIDRef.current !== requestID
      ) {
        return;
      }
      setMemoryCandidates(filterPendingMemoryCandidates(candidates));
    } catch {
      if (
        currentSessionIDRef.current !== sessionId ||
        memoryCandidatesRefreshRequestIDRef.current !== requestID
      ) {
        return;
      }
      setMemoryCandidatesError("Failed to load memory candidates.");
    } finally {
      if (
        currentSessionIDRef.current === sessionId &&
        memoryCandidatesRefreshRequestIDRef.current === requestID
      ) {
        setMemoryCandidatesLoading(false);
      }
    }
  }

  useEffect(() => {
    void refreshHITLTickets();
  }, []);

  useEffect(() => {
    const timer = setInterval(() => {
      void refreshHITLTickets(false);
    }, hitlPollIntervalMs);

    return () => {
      clearInterval(timer);
    };
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
        setConnectionError(message);
      },
      onMessage: (message: SessionSocketMessage) => {
        if (cancelled) {
          return;
        }

        if (message.type === "command.accepted") {
          setSubmitError(null);
          setPendingWSCommandState(null);
          lastWSCommandRef.current = null;
        }
        if (message.type === "command.error") {
          const pendingCommand = pendingWSCommandRef.current ?? lastWSCommandRef.current;
          if (pendingCommand === "continue_composer") {
            setSubmitError("Failed to continue turn.");
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
    memoryCandidatesRefreshRequestIDRef.current = 0;
    setMemoryCandidates([]);
    setMemoryCandidatesError(null);
    setMemoryCandidatesLoading(false);
    memoryCandidateActionPendingIDsRef.current = new Set();
    setMemoryCandidateActionPendingIDs([]);
    setMemoryCandidateActionError(null);

    if (!sessionId) {
      return;
    }

    void refreshMemoryCandidates(sessionId);
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

    requestRunDetail(sessionId, focusedRunId);
  }, [viewState.currentSessionId, viewState.focusedRunId, viewState.snapshot]);

  const snapshot = viewState.snapshot;
  const currentTurn = selectCurrentTurn(snapshot);
  const activeRuns = selectActiveRuns(snapshot);
  const focusedRun = selectFocusedRun(snapshot, viewState.focusedRunId);
  const conversationMessages = buildConversationMessages(viewState, pendingOptimisticMessage);
  const waitingForInput = isWaitingForInput(viewState);
  const submitPending = startTurnPending || pendingWSCommand === "continue_composer";
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
  const chatHeaderTitle = snapshot?.title || "Chat";
  const chatHeaderMeta = currentTurn
    ? `Turn ${currentTurn.turnId} · ${humanizeState(selectDisplayState(currentTurn, focusedRun))}`
    : connectionStatusMessage;
  const summaryStrip = buildSummaryStrip(currentTurn, focusedRun);
  const inlineStatus = buildInlineStatus(currentTurn, focusedRun, conversationMessages);

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

    setPendingOptimisticMessage({
      sessionId: viewState.currentSessionId,
      message: createOptimisticUserMessage(message),
    });
    setSubmitError(null);
    if (waitingForInput) {
      setPendingWSCommandState("continue_composer");
      const sessionId = await ensureSession().catch(() => null);
      if (!sessionId) {
        setPendingWSCommandState(null);
        return;
      }
      setPendingOptimisticMessage((current) =>
        current ? { ...current, sessionId } : current,
      );
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

    setPendingOptimisticMessage((current) =>
      current ? { ...current, sessionId } : current,
    );
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

  async function handleAcceptMemoryCandidate(candidateId: string) {
    const sessionId = viewState.currentSessionId;
    if (!sessionId) {
      return;
    }

    if (memoryCandidateActionPendingIDsRef.current.has(candidateId)) {
      return;
    }
    memoryCandidateActionPendingIDsRef.current.add(candidateId);
    setMemoryCandidateActionPendingIDs(Array.from(memoryCandidateActionPendingIDsRef.current));

    setMemoryCandidateActionError(null);
    try {
      await acceptMemoryCandidate(candidateId);
      if (currentSessionIDRef.current !== sessionId) {
        return;
      }
      await refreshMemoryCandidates(sessionId);
    } catch {
      if (currentSessionIDRef.current !== sessionId) {
        return;
      }
      setMemoryCandidateActionError("Failed to update candidate.");
    } finally {
      if (currentSessionIDRef.current === sessionId) {
        memoryCandidateActionPendingIDsRef.current.delete(candidateId);
        setMemoryCandidateActionPendingIDs(Array.from(memoryCandidateActionPendingIDsRef.current));
      }
    }
  }

  async function handleRejectMemoryCandidate(candidateId: string) {
    const sessionId = viewState.currentSessionId;
    if (!sessionId) {
      return;
    }

    if (memoryCandidateActionPendingIDsRef.current.has(candidateId)) {
      return;
    }
    memoryCandidateActionPendingIDsRef.current.add(candidateId);
    setMemoryCandidateActionPendingIDs(Array.from(memoryCandidateActionPendingIDsRef.current));

    setMemoryCandidateActionError(null);
    try {
      await rejectMemoryCandidate(candidateId);
      if (currentSessionIDRef.current !== sessionId) {
        return;
      }
      await refreshMemoryCandidates(sessionId);
    } catch {
      if (currentSessionIDRef.current !== sessionId) {
        return;
      }
      setMemoryCandidateActionError("Failed to update candidate.");
    } finally {
      if (currentSessionIDRef.current === sessionId) {
        memoryCandidateActionPendingIDsRef.current.delete(candidateId);
        setMemoryCandidateActionPendingIDs(Array.from(memoryCandidateActionPendingIDsRef.current));
      }
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
      waitingForInput={waitingForInput}
      chatHeaderTitle={chatHeaderTitle}
      chatHeaderMeta={chatHeaderMeta}
      summaryStripStatus={summaryStrip.status}
      summaryStripTone={summaryStrip.tone}
      inlineStatus={inlineStatus}
      submitPending={submitPending}
      submitError={submitError}
      runs={snapshot?.runs.length ? snapshot.runs : activeRuns}
      focusedRunId={focusedRun?.runId ?? viewState.focusedRunId}
      hitlTickets={hitlTickets}
      hitlLoading={hitlLoading}
      memoryCandidates={memoryCandidates}
      memoryCandidatesLoading={memoryCandidatesLoading}
      memoryCandidatesError={memoryCandidatesError}
      memoryCandidateActionError={memoryCandidateActionError}
      memoryCandidateActionPendingIds={memoryCandidateActionPendingIds}
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
            preserveRecentSessionOrder: true,
          }),
        );
      }}
      onSearchChange={setSearchQuery}
      onFilterChange={setFilter}
      onSubmitMessage={(message) => {
        void handleSubmit(message).catch(() => {});
      }}
      onRetryMemoryCandidates={() => {
        if (!viewState.currentSessionId) {
          return;
        }
        void refreshMemoryCandidates(viewState.currentSessionId);
      }}
      onAcceptMemoryCandidate={(candidateId) => {
        void handleAcceptMemoryCandidate(candidateId);
      }}
      onRejectMemoryCandidate={(candidateId) => {
        void handleRejectMemoryCandidate(candidateId);
      }}
      onRefreshHITL={() => {
        void refreshHITLTickets();
      }}
      onOpenHITLContext={handleOpenHITLContext}
      onRespondHITL={(ticketId, instruction) => handleRespondHITL(ticketId, instruction)}
      onDismissHITL={(ticketId) => handleDismissHITL(ticketId)}
      onCloseDebug={() => setDebugOpen(false)}
    />
  );
}
