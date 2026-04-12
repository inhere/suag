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
