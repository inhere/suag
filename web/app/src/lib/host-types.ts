export type SessionStatus = "OPEN" | "RUNNING" | "WAITING" | "ARCHIVED";

export type ExternalTaskSummary = {
  externalTaskId: string;
  sessionId: string;
  turnId: string;
  runId: string;
  adapter: string;
  remoteAgentName: string;
  remoteSessionId: string;
  remoteTaskId: string;
  state: string;
  lastHeartbeatAt: string;
  resultSummary: string;
  metadata: Record<string, unknown>;
};

export type SessionSummary = {
  sessionId: string;
  title: string;
  status: SessionStatus;
};

export type SessionDetail = SessionSummary & {
  lastTurnId: string;
  externalTasks: ExternalTaskSummary[];
};

export type StartTurnInput = {
  agentId: string;
  message: string;
  initiatorType: string;
};

export type StartTurnResult = {
  sessionId: string;
  turnId: string;
  runId: string;
};

export type StateSnapshotTurn = {
  turnId: string;
  sessionId: string;
  primaryRunId: string;
  currentRunId: string;
  latestRunId: string;
  latestAgentId: string;
  displayState: string;
  latestEventType: string;
  latestMessagePreview: string;
  eventCount: number;
  runCount: number;
  activeRunCount: number;
  completedRunCount: number;
  failedRunCount: number;
  activeRunIds: string[];
  completedRunIds: string[];
  failedRunIds: string[];
  hasExternalWork: boolean;
  updatedAt: string;
};

export type StateSnapshotRun = {
  runId: string;
  turnId: string;
  sessionId: string;
  agentId: string;
  displayState: string;
  latestEventType: string;
  latestMessageId: string;
  latestMessageText: string;
  summary: string;
  externalTaskIds: string[];
  eventCount: number;
  updatedAt: string;
};

export type StateSnapshotExternalActivity = {
  externalTaskId: string;
  sessionId: string;
  turnId: string;
  runId: string;
  agentId: string;
  activityId: string;
  kind: string;
  status: string;
  title: string;
  detail: string;
  startedAt: string;
  updatedAt: string;
};

export type StateSnapshotExternalMessage = {
  externalTaskId: string;
  messageId: string;
  sessionId: string;
  turnId: string;
  runId: string;
  agentId: string;
  content: string;
  started: boolean;
  completed: boolean;
  lastEventType: string;
  lastSequence: number;
  updatedAt: string;
};

export type HostEvent = {
  type: string;
  name?: string;
  messageId?: string;
  delta?: string;
  sequence?: number;
  value?: Record<string, unknown>;
  snapshot?: StateSnapshotV0;
};

export type StateSnapshotV0 = {
  snapshotVersion: "v0";
  snapshotSchema: "axon.host.state.v0";
  generatedAt: string;
  threadId: string;
  sessionId: string;
  title: string;
  status: string;
  turnId: string;
  lastTurnId: string;
  currentRunId: string;
  currentAgentId: string;
  latestEventType: string;
  eventCount: number;
  turns: StateSnapshotTurn[];
  runs: StateSnapshotRun[];
  externalTasks: ExternalTaskSummary[];
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
  events: HostEvent[];
};

export type StateSnapshotEvent = {
  type: "STATE_SNAPSHOT";
  snapshot: StateSnapshotV0;
};

export type ConnectedEvent = {
  type: "connected";
  session_id: string;
  trace_id?: string;
  wildcard?: boolean;
};

export type CommandAcceptedEvent = {
  type: "command.accepted";
  command: string;
  session_id: string;
};

export type CommandErrorEvent = {
  type: "command.error";
  error: string;
};

export type SessionSocketMessage =
  | ConnectedEvent
  | CommandAcceptedEvent
  | CommandErrorEvent
  | StateSnapshotEvent
  | HostEvent;
