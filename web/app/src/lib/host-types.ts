export type SessionStatus = "OPEN" | "RUNNING" | "WAITING" | "ARCHIVED";

export type HITLTicket = {
  id: string;
  agentId: string;
  taskId: string;
  sessionId: string;
  turnId: string;
  runId: string;
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

export type MemoryCandidate = {
  candidateId: string;
  sessionId: string;
  turnId: string;
  runId: string;
  sourceRecapId: string;
  target: string;
  kind: string;
  content: string;
  score: number;
  status: string;
  createdAt: string;
  metadata: Record<string, unknown>;
};

export type SessionSummary = {
  sessionId: string;
  title: string;
  status: SessionStatus;
};

export type SessionListEntry = SessionSummary & {
  lastTurnId: string;
  updatedAt: string;
  preview: string;
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
  inputSummary?: string;
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
  waitMetadata?: Record<string, unknown>;
  loopMetadata?: Record<string, unknown>;
  memoryRecall?: Record<string, unknown>;
  turnOutcome?: string;
  plannerHint?: PlannerHint;
  nextActionHint?: Record<string, unknown>;
  eventCount: number;
  updatedAt: string;
};

export type PlannerHint = {
  stage?: string;
  summary?: string;
  metadata?: {
    next_turn_plan?: NextTurnPlan;
    resolved_tool_refs?: ToolRefHint[];
    [key: string]: unknown;
  };
  [key: string]: unknown;
};

export type ToolRefHint = {
  name?: string;
  source?: string;
  provider_id?: string;
  [key: string]: unknown;
};

export type NextTurnPlan = {
  tool_mode?: string;
  tool_allowlist?: string[];
  skill_names?: string[];
  resource_hint_count?: number;
  recall_hit?: boolean;
  [key: string]: unknown;
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
  payload?: Record<string, unknown>;
  lastPayload?: Record<string, unknown>;
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
  turnId?: string;
  sessionId?: string;
  agentId?: string;
  threadId?: string;
  role?: string;
  payload?: Record<string, unknown>;
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
