import { describe, expect, it } from "vitest";
import type { StateSnapshotV0 } from "../../lib/host-types";
import { buildConversationTimeline } from "./conversation-timeline";

function makeSnapshot(): StateSnapshotV0 {
  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt: "2026-04-12T11:00:00Z",
    threadId: "sess_001",
    sessionId: "sess_001",
    title: "host runtime",
    status: "RUNNING",
    turnId: "turn_002",
    lastTurnId: "turn_002",
    currentRunId: "run_002",
    currentAgentId: "default",
    latestEventType: "run.external.message.delta",
    eventCount: 6,
    turns: [
      {
        turnId: "turn_001",
        sessionId: "sess_001",
        inputSummary: "第一轮：看一下结构",
        primaryRunId: "run_001",
        currentRunId: "run_001",
        latestRunId: "run_001",
        latestAgentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessagePreview: "第一轮完成",
        eventCount: 3,
        runCount: 1,
        activeRunCount: 0,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: [],
        completedRunIds: ["run_001"],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-12T10:00:00Z",
      },
      {
        turnId: "turn_002",
        sessionId: "sess_001",
        inputSummary: "第二轮：继续测试",
        primaryRunId: "run_002",
        currentRunId: "run_002",
        latestRunId: "run_002",
        latestAgentId: "default",
        displayState: "external_running",
        latestEventType: "run.external.message.delta",
        latestMessagePreview: "thinking...",
        eventCount: 3,
        runCount: 1,
        activeRunCount: 1,
        completedRunCount: 0,
        failedRunCount: 0,
        activeRunIds: ["run_002"],
        completedRunIds: [],
        failedRunIds: [],
        hasExternalWork: true,
        updatedAt: "2026-04-12T11:00:00Z",
      },
    ],
    runs: [
      {
        runId: "run_001",
        turnId: "turn_001",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessageId: "msg_001",
        latestMessageText: "第一轮完成",
        summary: "第一轮完成",
        externalTaskIds: [],
        plannerHint: {
          stage: "capability_review",
          summary: "review capability signals",
          metadata: {
            resolved_tool_refs: [
              { name: "file_read", source: "native", provider_id: "native" },
            ],
            next_turn_plan: {
              tool_mode: "read_only",
              tool_allowlist: ["file_read"],
              skill_names: ["office-hours"],
              recall_hit: true,
            },
          },
        },
        eventCount: 3,
        updatedAt: "2026-04-12T10:01:00Z",
      },
      {
        runId: "run_002",
        turnId: "turn_002",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "external_running",
        latestEventType: "run.external.message.delta",
        latestMessageId: "msg_002",
        latestMessageText: "thinking...",
        summary: "",
        externalTaskIds: ["ext_001"],
        eventCount: 3,
        updatedAt: "2026-04-12T11:01:00Z",
      },
    ],
    externalTasks: [],
    externalActivities: [
      {
        externalTaskId: "ext_001",
        sessionId: "sess_001",
        turnId: "turn_002",
        runId: "run_002",
        agentId: "default",
        activityId: "act_001",
        kind: "tool",
        status: "running",
        title: "file_read",
        detail: "reading package files",
        startedAt: "2026-04-12T11:01:00Z",
        updatedAt: "2026-04-12T11:02:00Z",
      },
    ],
    externalMessages: [
      {
        externalTaskId: "ext_001",
        messageId: "msg_002",
        sessionId: "sess_001",
        turnId: "turn_002",
        runId: "run_002",
        agentId: "default",
        content: "thinking...",
        started: true,
        completed: false,
        lastEventType: "run.external.message.delta",
        lastSequence: 1,
        updatedAt: "2026-04-12T11:01:30Z",
      },
    ],
    events: [],
  };
}

describe("buildConversationTimeline", () => {
  it("builds a scrollable agent-chat timeline from all turns and run activity", () => {
    const timeline = buildConversationTimeline(makeSnapshot());

    expect(timeline.map((entry) => entry.id)).toEqual([
      "turn_001:user",
      "run_001:status",
      "run_001:assistant",
      "run_001:capability-plan",
      "turn_002:user",
      "run_002:status",
      "msg_002:thinking",
      "act_001:activity",
    ]);
    expect(timeline).toContainEqual(
      expect.objectContaining({
        kind: "activity",
        title: "Capability plan",
        detail: expect.stringContaining("skills office-hours"),
      }),
    );
    expect(timeline).toContainEqual(
      expect.objectContaining({
        kind: "activity",
        title: "Thinking",
        detail: "thinking...",
      }),
    );
    expect(timeline).toContainEqual(
      expect.objectContaining({
        kind: "activity",
        title: "file_read",
        detail: "reading package files",
      }),
    );
  });

  it("keeps optimistic user input visible until snapshot catches up", () => {
    const pending = {
      sessionId: "sess_001",
      message: {
        id: "optimistic",
        kind: "message" as const,
        role: "user" as const,
        text: "新的输入",
        timestamp: "19:00",
      },
    };

    const timeline = buildConversationTimeline(makeSnapshot(), pending, "sess_001");

    expect(timeline[0]).toEqual(pending.message);
  });
});
