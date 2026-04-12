import type { SessionSocketMessage } from "./host-types";

export type ContinueTurnCommand = {
  message: string;
  agentId: string;
  initiatorType: string;
};

export type ConnectSessionSocketOptions = {
  sessionId: string;
  onMessage: (message: SessionSocketMessage) => void;
  createWebSocket?: () => WebSocket;
};

export type SessionSocketConnection = {
  close: () => void;
  sendContinueTurn: (command: ContinueTurnCommand) => void;
};

function buildSocketURL(sessionId: string): string {
  return `/ws?session_id=${encodeURIComponent(sessionId)}`;
}

export function connectSessionSocket(
  options: ConnectSessionSocketOptions,
): SessionSocketConnection {
  const ws =
    options.createWebSocket?.() ?? new WebSocket(buildSocketURL(options.sessionId));

  ws.onmessage = (event) => {
    options.onMessage(JSON.parse(event.data) as SessionSocketMessage);
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
  };
}
