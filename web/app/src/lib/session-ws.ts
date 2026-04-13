import type { SessionSocketMessage } from "./host-types";

export type SessionSocketStatus = "connecting" | "connected" | "disconnected";

export type ContinueTurnCommand = {
  message: string;
  agentId: string;
  initiatorType: string;
};

export type StopTurnCommand = {
  turnId: string;
  reason?: string;
};

export type ConnectSessionSocketOptions = {
  sessionId: string;
  onMessage: (message: SessionSocketMessage) => void;
  onStatusChange?: (status: SessionSocketStatus) => void;
  onError?: (message: string) => void;
  createWebSocket?: () => WebSocket;
};

export type SessionSocketConnection = {
  close: () => void;
  sendContinueTurn: (command: ContinueTurnCommand) => void;
  sendStopTurn: (command: StopTurnCommand) => void;
};

function buildSocketURL(sessionId: string): string {
  return `/ws?session_id=${encodeURIComponent(sessionId)}`;
}

export function connectSessionSocket(
  options: ConnectSessionSocketOptions,
): SessionSocketConnection {
  options.onStatusChange?.("connecting");
  const ws =
    options.createWebSocket?.() ?? new WebSocket(buildSocketURL(options.sessionId));

  ws.onopen = () => {
    options.onStatusChange?.("connected");
  };
  ws.onmessage = (event) => {
    options.onMessage(JSON.parse(event.data) as SessionSocketMessage);
  };
  ws.onerror = () => {
    options.onError?.("WebSocket connection error.");
  };
  ws.onclose = () => {
    options.onStatusChange?.("disconnected");
  };

  return {
    close: () => ws.close(),
    sendContinueTurn: (command) => {
      ws.send(
        JSON.stringify({
          type: "continue_turn",
          session_id: options.sessionId,
          message: command.message,
          agent_id: command.agentId,
          initiator_type: command.initiatorType,
        }),
      );
    },
    sendStopTurn: (command) => {
      ws.send(
        JSON.stringify({
          type: "stop_turn",
          session_id: options.sessionId,
          turn_id: command.turnId,
          reason: command.reason,
        }),
      );
    },
  };
}
