import type {
  ExternalTaskSummary,
  RunDetail,
  SessionDetail,
  SessionSummary,
  StateSnapshotExternalActivity,
  StateSnapshotExternalMessage,
  StateSnapshotRun,
  StateSnapshotTurn,
  StartTurnInput,
  StartTurnResult,
} from "./host-types";

type FetchLike = typeof fetch;

type CreateSessionResponse = {
  session_id: string;
  title: string;
  status: SessionSummary["status"];
};

type ExternalTaskResponse = {
  external_task_id: string;
  session_id: string;
  turn_id: string;
  run_id: string;
  adapter: string;
  remote_agent_name: string;
  remote_session_id: string;
  remote_task_id: string;
  state: string;
  last_heartbeat_at: string;
  result_summary: string;
  metadata?: Record<string, unknown>;
};

type GetSessionResponse = {
  session_id: string;
  title: string;
  status: SessionSummary["status"];
  last_turn_id: string;
  turns?: StateSnapshotTurn[];
  runs?: StateSnapshotRun[];
  external_activities?: StateSnapshotExternalActivity[];
  external_messages?: StateSnapshotExternalMessage[];
  external_tasks: ExternalTaskResponse[];
};

type StartTurnResponse = {
  session_id: string;
  turn_id: string;
  run_id: string;
};

type GetRunDetailResponse = {
  session_id: string;
  turn_id: string;
  turn?: StateSnapshotTurn | null;
  run?: StateSnapshotRun | null;
  external_activities?: StateSnapshotExternalActivity[];
  external_messages?: StateSnapshotExternalMessage[];
  external_tasks?: ExternalTaskResponse[];
};

async function parseJSON<T>(response: Response): Promise<T> {
  if (!response.ok) {
    throw new Error(`request failed: ${response.status}`);
  }

  return (await response.json()) as T;
}

function mapExternalTaskResponse(input: ExternalTaskResponse): ExternalTaskSummary {
  return {
    externalTaskId: input.external_task_id,
    sessionId: input.session_id,
    turnId: input.turn_id,
    runId: input.run_id,
    adapter: input.adapter,
    remoteAgentName: input.remote_agent_name,
    remoteSessionId: input.remote_session_id,
    remoteTaskId: input.remote_task_id,
    state: input.state,
    lastHeartbeatAt: input.last_heartbeat_at,
    resultSummary: input.result_summary,
    metadata: input.metadata ?? {},
  };
}

function mapSessionResponse(input: CreateSessionResponse): SessionSummary {
  return {
    sessionId: input.session_id,
    title: input.title,
    status: input.status,
  };
}

function mapSessionDetailResponse(input: GetSessionResponse): SessionDetail {
  return {
    sessionId: input.session_id,
    title: input.title,
    status: input.status,
    lastTurnId: input.last_turn_id,
    turns: input.turns ?? [],
    runs: input.runs ?? [],
    externalActivities: input.external_activities ?? [],
    externalMessages: input.external_messages ?? [],
    externalTasks: input.external_tasks.map(mapExternalTaskResponse),
  };
}

function mapStartTurnResponse(input: StartTurnResponse): StartTurnResult {
  return {
    sessionId: input.session_id,
    turnId: input.turn_id,
    runId: input.run_id,
  };
}

function mapRunDetailResponse(input: GetRunDetailResponse): RunDetail {
  return {
    sessionId: input.session_id,
    turnId: input.turn_id,
    turn: input.turn ?? null,
    run: input.run ?? null,
    externalActivities: input.external_activities ?? [],
    externalMessages: input.external_messages ?? [],
    externalTasks: (input.external_tasks ?? []).map(mapExternalTaskResponse),
  };
}

export async function createSession(
  input: { title: string },
  fetchImpl: FetchLike = fetch,
): Promise<SessionSummary> {
  const response = await fetchImpl("/api/sessions", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
  });

  return mapSessionResponse(await parseJSON<CreateSessionResponse>(response));
}

export async function getSession(
  sessionId: string,
  fetchImpl: FetchLike = fetch,
): Promise<SessionDetail> {
  const response = await fetchImpl(`/api/sessions/${sessionId}`);
  return mapSessionDetailResponse(await parseJSON<GetSessionResponse>(response));
}

export async function startTurn(
  sessionId: string,
  input: StartTurnInput,
  fetchImpl: FetchLike = fetch,
): Promise<StartTurnResult> {
  const response = await fetchImpl(`/api/sessions/${sessionId}/turns`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      agent_id: input.agentId,
      message: input.message,
      initiator_type: input.initiatorType,
    }),
  });

  return mapStartTurnResponse(await parseJSON<StartTurnResponse>(response));
}

export async function getRunDetail(
  sessionId: string,
  runId: string,
  fetchImpl: FetchLike = fetch,
): Promise<RunDetail> {
  const response = await fetchImpl(`/api/sessions/${sessionId}/runs/${runId}`);
  return mapRunDetailResponse(await parseJSON<GetRunDetailResponse>(response));
}
