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
};

export type ConversationActivityEntry = {
  id: string;
  kind: "activity";
  title: string;
  detail: string;
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
  const snapshotUserTexts = new Set(
    snapshot?.turns
      .map((turn) => turn.inputSummary?.trim())
      .filter((text): text is string => !!text) ?? [],
  );

  if (
    optimisticMessage &&
    (optimisticMessage.sessionId == null || optimisticMessage.sessionId === currentSessionId) &&
    optimisticMessage.message.text.trim() &&
    !snapshotUserTexts.has(optimisticMessage.message.text.trim())
  ) {
    entries.push(optimisticMessage.message);
  }

  if (!snapshot) {
    return entries;
  }

  const runsByTurn = groupBy(snapshot.runs, (run) => run.turnId);
  const messagesByRun = groupBy(snapshot.externalMessages, (message) => message.runId);
  const activitiesByRun = groupBy(snapshot.externalActivities, (activity) => activity.runId);

  const sortedTurns = [...snapshot.turns].sort(compareByUpdatedAt);
  for (const turn of sortedTurns) {
    if (turn.inputSummary?.trim()) {
      entries.push({
        id: `${turn.turnId}:user`,
        kind: "message",
        role: "user",
        text: turn.inputSummary,
        timestamp: formatConversationTimestamp(turn.updatedAt || snapshot.generatedAt),
      });
    }

    const sortedRuns = [...(runsByTurn.get(turn.turnId) ?? [])].sort(compareByUpdatedAt);
    for (const run of sortedRuns) {
      entries.push(runStatusEntry(run));

      const runMessages = [...(messagesByRun.get(run.runId) ?? [])].sort(compareByUpdatedAt);
      for (const message of runMessages) {
        if (!message.content.trim()) {
          continue;
        }
        entries.push({
          id: `${message.messageId}:thinking`,
          kind: "activity",
          title: message.completed ? "Thought" : "Thinking",
          detail: message.content,
          tone: message.completed ? "muted" : "running",
          timestamp: formatConversationTimestamp(message.updatedAt || snapshot.generatedAt),
        });
      }

      const runActivities = [...(activitiesByRun.get(run.runId) ?? [])].sort(compareByUpdatedAt);
      for (const activity of runActivities) {
        entries.push(externalActivityEntry(activity, snapshot.generatedAt));
      }

      const assistantText = firstNonEmpty(run.summary, run.latestMessageText);
      if (assistantText && !runMessages.some((message) => message.content.trim() === assistantText)) {
        entries.push({
          id: `${run.runId}:assistant`,
          kind: "message",
          role: "assistant",
          text: assistantText,
          timestamp: formatConversationTimestamp(run.updatedAt || snapshot.generatedAt),
        });
      }

      const planEntry = capabilityPlanEntry(run);
      if (planEntry) {
        entries.push(planEntry);
      }
    }
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
    tone: toneFromRunState(activity.status),
    timestamp: formatConversationTimestamp(activity.updatedAt || fallbackTime),
  };
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
