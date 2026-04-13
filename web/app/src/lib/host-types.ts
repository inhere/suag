export type SessionStatus = "OPEN" | "RUNNING" | "WAITING" | "ARCHIVED";

export type HITLTicket = {
  id: string;
  agentId: string;
  taskId: string;
  reason: string;
  context: string;
  status: string;
  createdAt: string;
};

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
  turns: StateSnapshotTurn[];
  runs: StateSnapshotRun[];
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
  externalTasks: ExternalTaskSummary[];
};

export type RunDetail = {
  sessionId: string;
  turnId: string;
  turn: StateSnapshotTurn | null;
  run: StateSnapshotRun | null;
  externalActivities: StateSnapshotExternalActivity[];
  externalMessages: StateSnapshotExternalMessage[];
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
  timestamp?: number;
  runId?: string;
  threadId?: string;
  role?: string;
  result?: Record<string, unknown>;
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

export type RunStartedEvent = {
  type: "RUN_STARTED";
  threadId: string;
  runId: string;
  timestamp?: number;
};

export type RunFinishedEvent = {
  type: "RUN_FINISHED";
  runId: string;
  threadId: string;
  timestamp?: number;
  result?: Record<string, unknown>;
};

export type RunErrorEvent = {
  type: "RUN_ERROR";
  runId: string;
  message: string;
  timestamp?: number;
};

export type RunCanceledEvent = {
  type: "RUN_CANCELED";
  runId: string;
  message: string;
  timestamp?: number;
};

export type ActivitySnapshotEvent = {
  type: "ACTIVITY_SNAPSHOT";
  messageId: string;
  activityType: string;
  content: Record<string, unknown>;
  replace?: boolean;
  timestamp?: number;
};

export type TextMessageStartEvent = {
  type: "TEXT_MESSAGE_START";
  messageId: string;
  role?: string;
  timestamp?: number;
};

export type TextMessageContentEvent = {
  type: "TEXT_MESSAGE_CONTENT";
  messageId: string;
  delta: string;
  timestamp?: number;
};

export type TextMessageEndEvent = {
  type: "TEXT_MESSAGE_END";
  messageId: string;
  timestamp?: number;
};

export type CustomEvent = {
  type: "CUSTOM";
  name: string;
  value?: Record<string, unknown>;
  timestamp?: number;
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
  | RunStartedEvent
  | RunFinishedEvent
  | RunErrorEvent
  | RunCanceledEvent
  | ActivitySnapshotEvent
  | TextMessageStartEvent
  | TextMessageContentEvent
  | TextMessageEndEvent
  | CustomEvent
  | HostEvent;
