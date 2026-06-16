import { describe, expect, it, vi } from "vitest";
import { connectSessionSocket } from "./session-ws";

class FakeWebSocket {
  onopen: (() => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  sent: string[] = [];
  closed = false;

  send(payload: string) {
    this.sent.push(payload);
  }

  close() {
    this.closed = true;
    this.onclose?.();
  }

  emitMessage(payload: string) {
    if (!this.onmessage) {
      return;
    }

    this.onmessage({ data: payload } as MessageEvent<string>);
  }

  emitOpen() {
    this.onopen?.();
  }

  emitClose() {
    this.onclose?.();
  }

  emitError() {
    this.onerror?.();
  }
}

describe("session-ws", () => {
  it("subscribes to a session websocket and forwards parsed messages", () => {
    const onMessage = vi.fn();
    const ws = new FakeWebSocket();

    const socket = connectSessionSocket({
      sessionId: "sess_001",
      createWebSocket: () => ws as unknown as WebSocket,
      onMessage,
    });

    ws.emitMessage(
      JSON.stringify({
        type: "STATE_SNAPSHOT",
        snapshot: {
          snapshotVersion: "v0",
          snapshotSchema: "suag.host.state.v0",
          generatedAt: "2026-04-12T08:00:00Z",
          threadId: "sess_001",
          sessionId: "sess_001",
          title: "demo",
          status: "RUNNING",
          turnId: "turn_001",
          lastTurnId: "turn_001",
          currentRunId: "run_001",
          currentAgentId: "default",
          latestEventType: "run.started",
          eventCount: 2,
          turns: [],
          runs: [],
          externalTasks: [],
          externalActivities: [],
          externalMessages: [],
          events: [],
        },
      }),
    );

    expect(onMessage).toHaveBeenCalledWith(
      expect.objectContaining({ type: "STATE_SNAPSHOT" }),
    );

    socket.close();
    expect(ws.closed).toBe(true);
  });

  it("reports websocket connection lifecycle status and errors", () => {
    const onStatusChange = vi.fn();
    const onError = vi.fn();
    const ws = new FakeWebSocket();

    connectSessionSocket({
      sessionId: "sess_001",
      createWebSocket: () => ws as unknown as WebSocket,
      onMessage: vi.fn(),
      onStatusChange,
      onError,
    });

    expect(onStatusChange).toHaveBeenNthCalledWith(1, "connecting");

    ws.emitOpen();
    ws.emitError();
    ws.emitClose();

    expect(onStatusChange).toHaveBeenNthCalledWith(2, "connected");
    expect(onStatusChange).toHaveBeenNthCalledWith(3, "disconnected");
    expect(onError).toHaveBeenCalledWith("WebSocket connection error.");
  });

  it("does not report disconnected when the socket is closed intentionally", () => {
    const onStatusChange = vi.fn();
    const ws = new FakeWebSocket();

    const socket = connectSessionSocket({
      sessionId: "sess_001",
      createWebSocket: () => ws as unknown as WebSocket,
      onMessage: vi.fn(),
      onStatusChange,
    });

    socket.close();

    expect(onStatusChange).toHaveBeenCalledTimes(1);
    expect(onStatusChange).toHaveBeenCalledWith("connecting");
  });

  it("sends continue_turn commands over the existing websocket", () => {
    const ws = new FakeWebSocket();

    const socket = connectSessionSocket({
      sessionId: "sess_001",
      createWebSocket: () => ws as unknown as WebSocket,
      onMessage: vi.fn(),
    });

    socket.sendContinueTurn({
      message: "continue with more detail",
      agentId: "default",
      initiatorType: "user",
    });

    expect(ws.sent).toEqual([
      JSON.stringify({
        type: "continue_turn",
        session_id: "sess_001",
        message: "continue with more detail",
        agent_id: "default",
        initiator_type: "user",
      }),
    ]);
  });

  it("sends stop_turn commands over the existing websocket", () => {
    const ws = new FakeWebSocket();

    const socket = connectSessionSocket({
      sessionId: "sess_001",
      createWebSocket: () => ws as unknown as WebSocket,
      onMessage: vi.fn(),
    });

    socket.sendStopTurn({
      turnId: "turn_001",
    });

    expect(ws.sent).toEqual([
      JSON.stringify({
        type: "stop_turn",
        session_id: "sess_001",
        turn_id: "turn_001",
      }),
    ]);
  });
});
