import type {
  ExternalTaskSummary,
  HITLTicket,
  MemoryCandidate,
  RunDetail,
  SessionDetail,
  SessionListEntry,
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

type ListSessionsResponseItem = {
  session_id: string;
  title: string;
  status: SessionSummary["status"];
  last_turn_id: string;
  updated_at: string;
  preview: string;
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

type HITLTicketResponse = {
  ID: string;
  AgentID: string;
  TaskID: string;
  SessionID?: string;
  TurnID?: string;
  RunID?: string;
  Reason: string;
  Context: string;
  Status: string;
  CreatedAt: string;
};

type MemoryCandidateResponse = {
  candidate_id: string;
  session_id: string;
  turn_id: string;
  run_id: string;
  source_recap_id: string;
  target: string;
  kind: string;
  content: string;
  score: number;
  status: string;
  created_at: string;
  metadata?: Record<string, unknown>;
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

function mapSessionListResponse(input: ListSessionsResponseItem): SessionListEntry {
  return {
    sessionId: input.session_id,
    title: input.title,
    status: input.status,
    lastTurnId: input.last_turn_id,
    updatedAt: input.updated_at,
    preview: input.preview,
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

function mapHITLTicketResponse(input: HITLTicketResponse): HITLTicket {
  return {
    id: input.ID,
    agentId: input.AgentID,
    taskId: input.TaskID,
    sessionId: input.SessionID ?? "",
    turnId: input.TurnID ?? "",
    runId: input.RunID ?? "",
    reason: input.Reason,
    context: input.Context,
    status: input.Status,
    createdAt: input.CreatedAt,
  };
}

function mapMemoryCandidateResponse(
  input: MemoryCandidateResponse,
): MemoryCandidate {
  return {
    candidateId: input.candidate_id,
    sessionId: input.session_id,
    turnId: input.turn_id,
    runId: input.run_id,
    sourceRecapId: input.source_recap_id,
    target: input.target,
    kind: input.kind,
    content: input.content,
    score: input.score,
    status: input.status,
    createdAt: input.created_at,
    metadata: input.metadata ?? {},
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

export async function listSessions(
  fetchImpl: FetchLike = fetch,
): Promise<SessionListEntry[]> {
  const response = await fetchImpl("/api/sessions");
  const payload = await parseJSON<ListSessionsResponseItem[]>(response);
  return payload.map(mapSessionListResponse);
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

export async function listHITLTickets(
  fetchImpl: FetchLike = fetch,
): Promise<HITLTicket[]> {
  const response = await fetchImpl("/api/hitl");
  const payload = await parseJSON<HITLTicketResponse[]>(response);
  return payload.map(mapHITLTicketResponse);
}

export async function respondHITLTicket(
  ticketId: string,
  instruction: string,
  fetchImpl: FetchLike = fetch,
): Promise<void> {
  const response = await fetchImpl(`/api/hitl/${ticketId}/respond`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ instruction }),
  });
  await parseJSON<{ status: string }>(response);
}

export async function dismissHITLTicket(
  ticketId: string,
  fetchImpl: FetchLike = fetch,
): Promise<void> {
  const response = await fetchImpl(`/api/hitl/${ticketId}/dismiss`, {
    method: "POST",
    headers: { "content-type": "application/json" },
  });
  await parseJSON<{ status: string }>(response);
}

export async function listMemoryCandidates(
  sessionId: string,
  fetchImpl: FetchLike = fetch,
): Promise<MemoryCandidate[]> {
  const response = await fetchImpl(
    `/api/sessions/${sessionId}/memory-candidates`,
  );
  const payload = await parseJSON<MemoryCandidateResponse[]>(response);
  return payload.map(mapMemoryCandidateResponse);
}

export async function acceptMemoryCandidate(
  candidateId: string,
  fetchImpl: FetchLike = fetch,
): Promise<void> {
  const response = await fetchImpl(
    `/api/memory-candidates/${candidateId}/accept`,
    {
      method: "POST",
      headers: { "content-type": "application/json" },
    },
  );
  await parseJSON<{ status: string }>(response);
}

export async function rejectMemoryCandidate(
  candidateId: string,
  fetchImpl: FetchLike = fetch,
): Promise<void> {
  const response = await fetchImpl(
    `/api/memory-candidates/${candidateId}/reject`,
    {
      method: "POST",
      headers: { "content-type": "application/json" },
    },
  );
  await parseJSON<{ status: string }>(response);
}
