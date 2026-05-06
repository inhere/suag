import type {
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
  StateSnapshotV0,
  ToolRefHint,
} from "../../lib/host-types";

export type ConversationMessageEntry = {
  id: string;
  kind: "message";
  role: "user" | "assistant";
  text: string;
  timestamp?: string;
  tokenUsage?: TokenUsage | null;
};

export type TokenUsage = {
  inputTokens?: number;
  outputTokens?: number;
  totalTokens?: number;
};

export type ConversationActivityEntry = {
  id: string;
  kind: "activity";
  title: string;
  detail: string;
  details?: Record<string, unknown>;
  tone?: "running" | "waiting" | "success" | "attention" | "muted";
  timestamp?: string;
};

export type ConversationTimelineEntry =
  | ConversationMessageEntry
  | ConversationActivityEntry;

export type PendingOptimisticConversationMessage = {
  sessionId: string | null;
  message: ConversationMessageEntry;
};

export function buildConversationTimeline(
  snapshot: StateSnapshotV0 | null,
  optimisticMessage?: PendingOptimisticConversationMessage | null,
  currentSessionId?: string | null,
): ConversationTimelineEntry[] {
  const entries: ConversationTimelineEntry[] = [];
  const turnInputFallbacks = buildTurnInputFallbacks(snapshot?.events ?? []);
  const snapshotUserTexts = new Set(
    snapshot?.turns
      .map((turn) => turnUserText(turn, turnInputFallbacks))
      .filter((text): text is string => !!text) ?? [],
  );
  const shouldShowOptimistic =
    optimisticMessage &&
    (optimisticMessage.sessionId == null || optimisticMessage.sessionId === currentSessionId) &&
    optimisticMessage.message.text.trim() &&
    !snapshotUserTexts.has(optimisticMessage.message.text.trim());

  if (!snapshot) {
    if (shouldShowOptimistic) {
      entries.push(optimisticMessage.message);
    }
    return entries;
  }

  const runsByTurn = groupBy(snapshot.runs, (run) => run.turnId);
  const messagesByRun = groupBy(snapshot.externalMessages, (message) => message.runId);
  const activitiesByRun = groupBy(snapshot.externalActivities, (activity) => activity.runId);

  const sortedTurns = [...snapshot.turns].sort(compareByUpdatedAt);
  for (const turn of sortedTurns) {
    const userText = turnUserText(turn, turnInputFallbacks);
    if (userText) {
      entries.push({
        id: `${turn.turnId}:user`,
        kind: "message",
        role: "user",
        text: userText,
        timestamp: formatConversationTimestamp(turn.updatedAt || snapshot.generatedAt),
      });
    }

    const sortedRuns = [...(runsByTurn.get(turn.turnId) ?? [])].sort(compareByUpdatedAt);
    for (const run of sortedRuns) {
      entries.push(runStatusEntry(run));
      const runActivities = [...(activitiesByRun.get(run.runId) ?? [])].sort(compareByUpdatedAt);
      const tokenUsage = tokenUsageFromActivities(runActivities);

      const runMessages = [...(messagesByRun.get(run.runId) ?? [])].sort(compareByUpdatedAt);
      if (!run.summary?.trim()) {
        for (const message of runMessages) {
          const content = message.content.trim();
          if (!content) {
            continue;
          }
          entries.push({
            id: `${message.messageId}:assistant`,
            kind: "message",
            role: "assistant",
            text: message.content,
            timestamp: formatConversationTimestamp(message.updatedAt || snapshot.generatedAt),
            tokenUsage,
          });
        }
      }

      for (const activity of runActivities) {
        entries.push(externalActivityEntry(activity, snapshot.generatedAt));
      }

      const assistantText = firstNonEmpty(run.summary, run.latestMessageText);
      if (assistantText && shouldUseRunAssistantText(run, runMessages)) {
        entries.push({
          id: `${run.runId}:assistant`,
          kind: "message",
          role: "assistant",
          text: assistantText,
          timestamp: formatConversationTimestamp(run.updatedAt || snapshot.generatedAt),
          tokenUsage,
        });
      }

      const planEntry = capabilityPlanEntry(run);
      if (planEntry) {
        entries.push(planEntry);
      }
    }
  }

  if (shouldShowOptimistic) {
    entries.push(optimisticMessage.message);
  }

  return entries;
}

export function formatConversationTimestamp(value: string | undefined): string {
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

function runStatusEntry(run: StateSnapshotRun): ConversationActivityEntry {
  return {
    id: `${run.runId}:status`,
    kind: "activity",
    title: run.displayState.replaceAll("_", " "),
    detail: `${run.agentId || "agent"} · ${run.runId}`,
    tone: toneFromRunState(run.displayState),
    timestamp: formatConversationTimestamp(run.updatedAt),
  };
}

function externalActivityEntry(
  activity: StateSnapshotExternalActivity,
  fallbackTime: string,
): ConversationActivityEntry {
  return {
    id: `${activity.activityId}:activity`,
    kind: "activity",
    title: activity.title || activity.kind || "Activity",
    detail: activity.detail || activity.status,
    details: activityDetails(activity),
    tone: toneFromRunState(activity.status),
    timestamp: formatConversationTimestamp(activity.updatedAt || fallbackTime),
  };
}

function activityDetails(activity: StateSnapshotExternalActivity): Record<string, unknown> | undefined {
  const details = activity.lastPayload ?? activity.payload;
  if (!details || !Object.keys(details).length) {
    return undefined;
  }
  return details;
}

function shouldUseRunAssistantText(
  run: StateSnapshotRun,
  messages: StateSnapshotExternalMessage[],
): boolean {
  if (run.summary?.trim()) {
    return true;
  }
  return !messages.some((message) => message.content.trim());
}

function capabilityPlanEntry(run: StateSnapshotRun): ConversationActivityEntry | null {
  const metadata = run.plannerHint?.metadata;
  if (!metadata) {
    return null;
  }

  const plan = metadata.next_turn_plan;
  const parts: string[] = [];
  if (plan?.tool_mode) {
    parts.push(`mode ${plan.tool_mode}`);
  }
  if (plan?.tool_allowlist?.length) {
    parts.push(`tools ${plan.tool_allowlist.join(", ")}`);
  }
  if (plan?.skill_names?.length) {
    parts.push(`skills ${plan.skill_names.join(", ")}`);
  }

  const refs = Array.isArray(metadata.resolved_tool_refs)
    ? formatToolRefs(metadata.resolved_tool_refs)
    : "";
  if (refs) {
    parts.push(`refs ${refs}`);
  }

  if (!parts.length) {
    return null;
  }

  return {
    id: `${run.runId}:capability-plan`,
    kind: "activity",
    title: "Capability plan",
    detail: parts.join(" · "),
    tone: "muted",
    timestamp: formatConversationTimestamp(run.updatedAt),
  };
}

function formatToolRefs(refs: ToolRefHint[]): string {
  return refs
    .map((ref) => {
      const name = ref.name?.trim();
      if (!name) {
        return "";
      }
      const provider = ref.provider_id?.trim() || ref.source?.trim();
      return provider ? `${provider}:${name}` : name;
    })
    .filter(Boolean)
    .join(", ");
}

function toneFromRunState(state: string): ConversationActivityEntry["tone"] {
  const normalized = state.toLowerCase();
  if (normalized.includes("failed") || normalized.includes("error")) {
    return "attention";
  }
  if (normalized.includes("waiting") || normalized.includes("input") || normalized.includes("auth")) {
    return "waiting";
  }
  if (normalized.includes("completed") || normalized.includes("finished")) {
    return "success";
  }
  if (normalized.includes("running") || normalized.includes("dispatch")) {
    return "running";
  }
  return "muted";
}

function firstNonEmpty(...values: Array<string | null | undefined>): string {
  for (const value of values) {
    if (value?.trim()) {
      return value;
    }
  }
  return "";
}

function tokenUsageFromActivities(
  activities: StateSnapshotExternalActivity[],
): TokenUsage | null {
  for (let idx = activities.length - 1; idx >= 0; idx--) {
    const activity = activities[idx];
    const payload = objectValue(activity.lastPayload) ?? objectValue(activity.payload);
    const usage = objectValue(payload?.usage);
    if (!usage) {
      continue;
    }

    const tokenUsage: TokenUsage = {
      inputTokens: numberValue(usage.inputTokens),
      outputTokens: numberValue(usage.outputTokens),
      totalTokens: numberValue(usage.totalTokens),
    };
    if (
      tokenUsage.inputTokens != null ||
      tokenUsage.outputTokens != null ||
      tokenUsage.totalTokens != null
    ) {
      return tokenUsage;
    }
  }
  return null;
}

function turnUserText(
  turn: { turnId: string; inputSummary?: string },
  fallbacks: Map<string, string>,
): string {
  return firstNonEmpty(turn.inputSummary, fallbacks.get(turn.turnId));
}

function buildTurnInputFallbacks(events: StateSnapshotV0["events"]): Map<string, string> {
  const inputs = new Map<string, string>();
  for (const event of events) {
    const value = objectValue(event.value);
    const payload = objectValue(event.payload) ?? objectValue(value?.payload);
    const turnId = firstNonEmpty(event.turnId, stringValue(value?.turnId));
    const message = firstNonEmpty(
      stringValue(payload?.message),
      stringValue(value?.message),
    );
    if (turnId && message && !inputs.has(turnId)) {
      inputs.set(turnId, message);
    }
  }
  return inputs;
}

function objectValue(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return null;
  }
  return value as Record<string, unknown>;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberValue(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  return undefined;
}

function groupBy<T>(items: T[], keyOf: (item: T) => string): Map<string, T[]> {
  const groups = new Map<string, T[]>();
  for (const item of items) {
    const key = keyOf(item);
    const group = groups.get(key) ?? [];
    group.push(item);
    groups.set(key, group);
  }
  return groups;
}

function compareByUpdatedAt(left: { updatedAt: string }, right: { updatedAt: string }): number {
  return left.updatedAt.localeCompare(right.updatedAt);
}
