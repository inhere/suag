import { afterEach, describe, expect, it, vi } from "vitest";
import { createSession, getSession, startTurn } from "./session-client";

describe("session-client", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("posts to /api/sessions and returns parsed session data", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        session_id: "sess_001",
        title: "demo",
        status: "OPEN",
      }),
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await createSession({ title: "demo" });

    expect(fetchMock).toHaveBeenCalledWith("/api/sessions", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ title: "demo" }),
    });
    expect(result).toEqual({
      sessionId: "sess_001",
      title: "demo",
      status: "OPEN",
    });
  });

  it("loads /api/sessions/{id} and returns parsed session detail", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          session_id: "sess_001",
          title: "demo",
          status: "RUNNING",
          last_turn_id: "turn_001",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "completed",
              latestEventType: "run.finished",
              latestMessagePreview: "done",
              eventCount: 3,
              runCount: 1,
              activeRunCount: 0,
              completedRunCount: 1,
              failedRunCount: 0,
              activeRunIds: [],
              completedRunIds: ["run_001"],
              failedRunIds: [],
              hasExternalWork: false,
              updatedAt: "2026-04-12T08:00:01Z",
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
              latestMessageId: "run_001:assistant",
              latestMessageText: "done",
              summary: "done",
              externalTaskIds: [],
              eventCount: 3,
              updatedAt: "2026-04-12T08:00:01Z",
            },
          ],
          external_activities: [
            {
              externalTaskId: "ext_001",
              sessionId: "sess_001",
              turnId: "turn_001",
              runId: "run_001",
              agentId: "default",
              activityId: "run_001:external",
              kind: "run_status",
              status: "completed",
              title: "run_status",
              detail: "completed",
              startedAt: "2026-04-12T08:00:00Z",
              updatedAt: "2026-04-12T08:00:01Z",
            },
          ],
          external_messages: [
            {
              externalTaskId: "ext_001",
              messageId: "msg_001",
              sessionId: "sess_001",
              turnId: "turn_001",
              runId: "run_001",
              agentId: "default",
              content: "thinking...",
              started: true,
              completed: true,
              lastEventType: "run.external.message.end",
              lastSequence: 3,
              updatedAt: "2026-04-12T08:00:01Z",
            },
          ],
          external_tasks: [
            {
              external_task_id: "ext_001",
              session_id: "sess_001",
              turn_id: "turn_001",
              run_id: "run_001",
              adapter: "codex",
              remote_agent_name: "codex-cli",
              remote_session_id: "remote_sess_001",
              remote_task_id: "remote_task_001",
              state: "running",
              last_heartbeat_at: "2026-04-12T08:00:00Z",
              result_summary: "",
              metadata: { protocol: "codex_app_server" },
            },
          ],
        }),
      }),
    );

    const result = await getSession("sess_001");

    expect(result.sessionId).toBe("sess_001");
    expect(result.lastTurnId).toBe("turn_001");
    expect(result.turns[0]?.displayState).toBe("completed");
    expect(result.runs[0]?.summary).toBe("done");
    expect(result.externalActivities[0]?.kind).toBe("run_status");
    expect(result.externalMessages[0]?.content).toBe("thinking...");
    expect(result.externalTasks[0]?.remoteAgentName).toBe("codex-cli");
  });

  it("posts to /api/sessions/{id}/turns and returns parsed turn identifiers", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        session_id: "sess_001",
        turn_id: "turn_002",
        run_id: "run_002",
      }),
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await startTurn("sess_001", {
      agentId: "default",
      message: "implement feature x",
      initiatorType: "user",
    });

    expect(fetchMock).toHaveBeenCalledWith("/api/sessions/sess_001/turns", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        agent_id: "default",
        message: "implement feature x",
        initiator_type: "user",
      }),
    });
    expect(result.runId).toBe("run_002");
  });
});
