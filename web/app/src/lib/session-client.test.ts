import { afterEach, describe, expect, it, vi } from "vitest";
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
} from "./session-client";

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
            waitMetadata: {},
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

  it("loads /api/sessions and returns parsed session summaries", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        {
          session_id: "sess_002",
          title: "latest session",
          status: "WAITING",
          last_turn_id: "turn_002",
          updated_at: "2026-04-13T07:00:00Z",
          preview: "needs review",
        },
        {
          session_id: "sess_001",
          title: "older session",
          status: "OPEN",
          last_turn_id: "turn_001",
          updated_at: "2026-04-13T06:00:00Z",
          preview: "done",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listSessions();

    expect(fetchMock).toHaveBeenCalledWith("/api/sessions");
    expect(result).toEqual([
      {
        sessionId: "sess_002",
        title: "latest session",
        status: "WAITING",
        lastTurnId: "turn_002",
        updatedAt: "2026-04-13T07:00:00Z",
        preview: "needs review",
      },
      {
        sessionId: "sess_001",
        title: "older session",
        status: "OPEN",
        lastTurnId: "turn_001",
        updatedAt: "2026-04-13T06:00:00Z",
        preview: "done",
      },
    ]);
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

  it("loads /api/sessions/{id}/runs/{runId} and returns parsed run detail", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          session_id: "sess_001",
          turn_id: "turn_001",
          run: {
            runId: "run_001",
            turnId: "turn_001",
            sessionId: "sess_001",
            agentId: "default",
            displayState: "completed",
            latestEventType: "run.finished",
            latestMessageId: "msg_001",
            latestMessageText: "thinking...",
            summary: "",
            externalTaskIds: [],
            waitMetadata: {
              wait_kind: "auth_required",
              policy: "manual_review",
            },
            eventCount: 4,
            updatedAt: "2026-04-12T08:00:01Z",
          },
          turn: {
            turnId: "turn_001",
            sessionId: "sess_001",
            primaryRunId: "run_001",
            currentRunId: "run_001",
            latestRunId: "run_001",
            latestAgentId: "default",
            displayState: "completed",
            latestEventType: "run.finished",
            latestMessagePreview: "thinking...",
            eventCount: 4,
            runCount: 1,
            activeRunCount: 0,
            completedRunCount: 1,
            failedRunCount: 0,
            activeRunIds: [],
            completedRunIds: ["run_001"],
            failedRunIds: [],
            hasExternalWork: true,
            updatedAt: "2026-04-12T08:00:01Z",
          },
          external_activities: [
            {
              externalTaskId: "",
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
              externalTaskId: "",
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
          external_tasks: [],
        }),
      }),
    );

    const result = await getRunDetail("sess_001", "run_001");

    expect(result.sessionId).toBe("sess_001");
    expect(result.turn?.turnId).toBe("turn_001");
    expect(result.run?.runId).toBe("run_001");
    expect(result.externalMessages[0]?.content).toBe("thinking...");
    expect(result.run?.waitMetadata).toEqual({
      wait_kind: "auth_required",
      policy: "manual_review",
    });
  });

  it("loads /api/hitl and returns parsed pending tickets", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        {
          ID: "hitl-001",
          AgentID: "default",
          TaskID: "task_001",
          Reason: "need approval",
          Context: "tool call needs confirmation",
          Status: "waiting",
          CreatedAt: "2026-04-13T06:00:00Z",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listHITLTickets();

    expect(fetchMock).toHaveBeenCalledWith("/api/hitl");
    expect(result).toEqual([
      {
        id: "hitl-001",
        agentId: "default",
        taskId: "task_001",
        sessionId: "",
        turnId: "",
        runId: "",
        reason: "need approval",
        context: "tool call needs confirmation",
        status: "waiting",
        createdAt: "2026-04-13T06:00:00Z",
      },
    ]);
  });

  it("posts a response to /api/hitl/{id}/respond", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: "ok" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await respondHITLTicket("hitl-001", "retry with safer args");

    expect(fetchMock).toHaveBeenCalledWith("/api/hitl/hitl-001/respond", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ instruction: "retry with safer args" }),
    });
  });

  it("posts a dismiss request to /api/hitl/{id}/dismiss", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ status: "ok" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    await dismissHITLTicket("hitl-001");

    expect(fetchMock).toHaveBeenCalledWith("/api/hitl/hitl-001/dismiss", {
      method: "POST",
      headers: { "content-type": "application/json" },
    });
  });

  it("loads /api/sessions/{id}/memory-candidates and returns parsed candidates", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        {
          candidate_id: "cand_001",
          session_id: "sess_001",
          turn_id: "turn_001",
          run_id: "run_001",
          source_recap_id: "recap_001",
          target: "MEMORY",
          kind: "fact",
          content: "user prefers concise responses",
          score: 0.9,
          status: "pending",
          created_at: "2026-04-17T09:00:00Z",
          metadata: { source: "recap" },
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listMemoryCandidates("sess_001");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/sessions/sess_001/memory-candidates",
    );
    expect(result).toEqual([
      {
        candidateId: "cand_001",
        sessionId: "sess_001",
        turnId: "turn_001",
        runId: "run_001",
        sourceRecapId: "recap_001",
        target: "MEMORY",
        kind: "fact",
        content: "user prefers concise responses",
        score: 0.9,
        status: "pending",
        createdAt: "2026-04-17T09:00:00Z",
        metadata: { source: "recap" },
      },
    ]);
  });

  it("falls back to empty metadata when memory candidate metadata is omitted", async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => [
        {
          candidate_id: "cand_002",
          session_id: "sess_001",
          turn_id: "turn_002",
          run_id: "run_002",
          source_recap_id: "recap_002",
          target: "MEMORY",
          kind: "preference",
          content: "prefers chinese output",
          score: 0.8,
          status: "pending",
          created_at: "2026-04-17T10:00:00Z",
        },
      ],
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await listMemoryCandidates("sess_001");

    expect(result[0]?.metadata).toEqual({});
  });

  it("posts accept request to /api/memory-candidates/{id}/accept", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true });
    vi.stubGlobal("fetch", fetchMock);

    await acceptMemoryCandidate("cand_001");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/memory-candidates/cand_001/accept",
      {
        method: "POST",
        headers: { "content-type": "application/json" },
      },
    );
  });

  it("rejects when /api/memory-candidates/{id}/accept returns non-2xx", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 500 });
    vi.stubGlobal("fetch", fetchMock);

    await expect(acceptMemoryCandidate("cand_001")).rejects.toThrow(
      "request failed: 500",
    );
  });

  it("posts reject request to /api/memory-candidates/{id}/reject", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true });
    vi.stubGlobal("fetch", fetchMock);

    await rejectMemoryCandidate("cand_001");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/memory-candidates/cand_001/reject",
      {
        method: "POST",
        headers: { "content-type": "application/json" },
      },
    );
  });

  it("rejects when /api/memory-candidates/{id}/reject returns non-2xx", async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 400 });
    vi.stubGlobal("fetch", fetchMock);

    await expect(rejectMemoryCandidate("cand_001")).rejects.toThrow(
      "request failed: 400",
    );
  });
});
