import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { SessionSocketMessage, StateSnapshotV0 } from "../lib/host-types";
import type { SessionSocketStatus } from "../lib/session-ws";

const createSessionMock = vi.fn();
const listSessionsMock = vi.fn();
const listHITLTicketsMock = vi.fn();
const respondHITLTicketMock = vi.fn();
const dismissHITLTicketMock = vi.fn();
const getSessionMock = vi.fn();
const startTurnMock = vi.fn();
const getRunDetailMock = vi.fn();
const sendContinueTurnMock = vi.fn();
const sendStopTurnMock = vi.fn();
const closeSocketMock = vi.fn();
const connectSessionSocketMock = vi.fn();

vi.mock("../lib/session-client", () => ({
  createSession: (...args: unknown[]) => createSessionMock(...args),
  listSessions: (...args: unknown[]) => listSessionsMock(...args),
  listHITLTickets: (...args: unknown[]) => listHITLTicketsMock(...args),
  respondHITLTicket: (...args: unknown[]) => respondHITLTicketMock(...args),
  dismissHITLTicket: (...args: unknown[]) => dismissHITLTicketMock(...args),
  getSession: (...args: unknown[]) => getSessionMock(...args),
  getRunDetail: (...args: unknown[]) => getRunDetailMock(...args),
  startTurn: (...args: unknown[]) => startTurnMock(...args),
}));

vi.mock("../lib/session-ws", () => ({
  connectSessionSocket: (...args: unknown[]) => connectSessionSocketMock(...args),
}));

function makeSnapshot(overrides: Partial<StateSnapshotV0> = {}): StateSnapshotV0 {
  return {
    snapshotVersion: "v0",
    snapshotSchema: "axon.host.state.v0",
    generatedAt: "2026-04-12T11:00:00Z",
    threadId: "sess_001",
    sessionId: "sess_001",
    title: "host runtime",
    status: "RUNNING",
    turnId: "turn_001",
    lastTurnId: "turn_001",
    currentRunId: "run_001",
    currentAgentId: "default",
    latestEventType: "run.started",
    eventCount: 2,
    turns: [
      {
        turnId: "turn_001",
        sessionId: "sess_001",
        primaryRunId: "run_001",
        currentRunId: "run_001",
        latestRunId: "run_001",
        latestAgentId: "default",
        displayState: "external_running",
        latestEventType: "run.started",
        latestMessagePreview: "正在整理页面结构",
        eventCount: 2,
        runCount: 1,
        activeRunCount: 1,
        completedRunCount: 0,
        failedRunCount: 0,
        activeRunIds: ["run_001"],
        completedRunIds: [],
        failedRunIds: [],
        hasExternalWork: true,
        updatedAt: "2026-04-12T11:00:00Z",
      },
    ],
    runs: [
      {
        runId: "run_001",
        turnId: "turn_001",
        sessionId: "sess_001",
        agentId: "default",
        displayState: "external_running",
        latestEventType: "run.started",
        latestMessageId: "msg_001",
        latestMessageText: "正在整理页面结构",
        summary: "",
        externalTaskIds: ["ext_001"],
        eventCount: 2,
        updatedAt: "2026-04-12T11:00:00Z",
      },
    ],
    externalTasks: [
      {
        externalTaskId: "ext_001",
        sessionId: "sess_001",
        turnId: "turn_001",
        runId: "run_001",
        adapter: "codex",
        remoteAgentName: "codex-cli",
        remoteSessionId: "remote_sess_001",
        remoteTaskId: "remote_task_001",
        state: "running",
        lastHeartbeatAt: "2026-04-12T11:00:00Z",
        resultSummary: "",
        metadata: { protocol: "codex_app_server" },
      },
    ],
    externalActivities: [],
    externalMessages: [
      {
        externalTaskId: "ext_001",
        messageId: "msg_001",
        sessionId: "sess_001",
        turnId: "turn_001",
        runId: "run_001",
        agentId: "default",
        content: "thinking...",
        started: true,
        completed: false,
        lastEventType: "run.external.message.delta",
        lastSequence: 2,
        updatedAt: "2026-04-12T11:00:00Z",
      },
    ],
    events: [],
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });

  return { promise, resolve, reject };
}

describe("App", () => {
  let onMessage: ((message: SessionSocketMessage) => void) | null;
  let onStatusChange: ((status: SessionSocketStatus) => void) | null;
  let onSocketError: ((message: string) => void) | null;
  let storage: Record<string, string>;

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    onMessage = null;
    onStatusChange = null;
    onSocketError = null;
    storage = {};
    createSessionMock.mockReset();
    listSessionsMock.mockReset();
    listHITLTicketsMock.mockReset();
    respondHITLTicketMock.mockReset();
    dismissHITLTicketMock.mockReset();
    getSessionMock.mockReset();
    getRunDetailMock.mockReset();
    startTurnMock.mockReset();
    sendContinueTurnMock.mockReset();
    sendStopTurnMock.mockReset();
    closeSocketMock.mockReset();
    connectSessionSocketMock.mockReset();

    createSessionMock.mockResolvedValue({
      sessionId: "sess_001",
      title: "host runtime",
      status: "OPEN",
    });
    listSessionsMock.mockResolvedValue([]);
    listHITLTicketsMock.mockResolvedValue([]);
    respondHITLTicketMock.mockResolvedValue(undefined);
    dismissHITLTicketMock.mockResolvedValue(undefined);
    getSessionMock.mockResolvedValue({
      sessionId: "sess_001",
      title: "host runtime",
      status: "OPEN",
      lastTurnId: "",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });
    startTurnMock.mockResolvedValue({
      sessionId: "sess_001",
      turnId: "turn_001",
      runId: "run_001",
    });
    getRunDetailMock.mockResolvedValue({
      sessionId: "sess_001",
      turnId: "turn_001",
      turn: null,
      run: null,
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });
    connectSessionSocketMock.mockImplementation((options: {
      onMessage: (message: SessionSocketMessage) => void;
      onStatusChange?: (status: SessionSocketStatus) => void;
      onError?: (message: string) => void;
    }) => {
      onMessage = options.onMessage;
      onStatusChange = options.onStatusChange ?? null;
      onSocketError = options.onError ?? null;
      return {
        close: closeSocketMock,
        sendContinueTurn: sendContinueTurnMock,
        sendStopTurn: sendStopTurnMock,
      };
    });

    vi.stubGlobal("localStorage", {
      getItem: vi.fn((key: string) => storage[key] ?? null),
      setItem: vi.fn((key: string, value: string) => {
        storage[key] = value;
      }),
      removeItem: vi.fn((key: string) => {
        delete storage[key];
      }),
    });
    vi.stubGlobal("prompt", vi.fn());
  });

  it("creates a session, starts a turn, and hydrates the workbench from websocket snapshots", async () => {
    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "请整理当前页面结构" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(createSessionMock).toHaveBeenCalledWith({ title: "host runtime" }),
    );
    await waitFor(() =>
      expect(startTurnMock).toHaveBeenCalledWith("sess_001", {
        agentId: "default",
        initiatorType: "user",
        message: "请整理当前页面结构",
      }),
    );
    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot(),
      });
    });

    expect(await screen.findByText("host runtime")).toBeInTheDocument();
    expect((await screen.findAllByText("thinking...")).length).toBeGreaterThan(0);
    expect(screen.getByText("run_001")).toBeInTheDocument();
    expect(screen.getAllByText("Turn turn_001").length).toBeGreaterThan(0);
    expect(screen.getByText("Run run_001")).toBeInTheDocument();
    await waitFor(() =>
      expect(storage["axon.web.workbench"]).toContain("\"currentSessionId\":\"sess_001\""),
    );
  });

  it("shows pending feedback while creating a session from the sidebar", async () => {
    const createDeferred = deferred<{
      sessionId: string;
      title: string;
      status: string;
    }>();
    createSessionMock.mockReturnValueOnce(createDeferred.promise);

    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "+ New Session" }));
    expect(await screen.findByRole("button", { name: "Creating..." })).toBeDisabled();

    await act(async () => {
      createDeferred.resolve({
        sessionId: "sess_100",
        title: "manual session",
        status: "OPEN",
      });
    });

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "+ New Session" })).toBeEnabled(),
    );
  });

  it("shows inline feedback when creating a session fails", async () => {
    createSessionMock.mockRejectedValueOnce(new Error("create failed"));

    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "+ New Session" }));

    expect(await screen.findByText("Failed to create session.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "+ New Session" })).toBeEnabled();
  });

  it("shows pending feedback while starting a new turn", async () => {
    const startDeferred = deferred<{
      sessionId: string;
      turnId: string;
      runId: string;
    }>();
    startTurnMock.mockReturnValueOnce(startDeferred.promise);

    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "先启动一个带提交反馈的新回合" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByRole("button", { name: "Starting..." })).toBeDisabled();

    await act(async () => {
      startDeferred.resolve({
        sessionId: "sess_001",
        turnId: "turn_001",
        runId: "run_001",
      });
    });

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Starting..." })).not.toBeInTheDocument(),
    );
  });

  it("shows inline feedback when starting a turn fails", async () => {
    startTurnMock.mockRejectedValueOnce(new Error("start failed"));

    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "启动失败提示用例" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    expect(await screen.findByText("Failed to start turn.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled();
  });

  it("shows websocket connection lifecycle status for the active session", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_002",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_002",
          title: "status session",
          status: "RUNNING",
          updatedAt: "2026-04-13T09:00:00Z",
          preview: "status preview",
        },
      ],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_002",
      title: "status session",
      status: "RUNNING",
      lastTurnId: "",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    expect(await screen.findByText("Session sess_002")).toBeInTheDocument();
    expect(screen.getByText("Turn none")).toBeInTheDocument();
    expect(screen.getByText("Run none")).toBeInTheDocument();
    expect(screen.getByText("Socket connecting")).toBeInTheDocument();
    expect(await screen.findByText("Session sess_002 connecting...")).toBeInTheDocument();

    await act(async () => {
      onStatusChange?.("connected");
    });
    expect(await screen.findByText("Socket connected")).toBeInTheDocument();
    expect(await screen.findByText("Session sess_002 connected.")).toBeInTheDocument();

    await act(async () => {
      onStatusChange?.("disconnected");
    });
    expect(await screen.findByText("Socket disconnected")).toBeInTheDocument();
    expect(await screen.findByText("Session sess_002 disconnected.")).toBeInTheDocument();
  });

  it("shows connection error feedback when session bootstrap fails", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_003",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_003",
          title: "broken session",
          status: "WAITING",
          updatedAt: "2026-04-13T09:10:00Z",
          preview: "broken preview",
        },
      ],
    });
    getSessionMock.mockRejectedValueOnce(new Error("boom"));

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to load session details.");

    await act(async () => {
      onSocketError?.("WebSocket connection error.");
    });
    expect(await screen.findByRole("alert")).toHaveTextContent("WebSocket connection error.");
  });

  it("retries the active session websocket automatically after disconnect", async () => {
    vi.useFakeTimers();
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_020",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_020",
          title: "retry session",
          status: "RUNNING",
          updatedAt: "2026-04-13T10:00:00Z",
          preview: "retry preview",
        },
      ],
    });
    getSessionMock.mockResolvedValue({
      sessionId: "sess_020",
      title: "retry session",
      status: "RUNNING",
      lastTurnId: "",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    expect(connectSessionSocketMock).toHaveBeenCalledWith(
      expect.objectContaining({ sessionId: "sess_020" }),
    );

    await act(async () => {
      onStatusChange?.("disconnected");
    });
    expect(screen.getByText("Session sess_020 disconnected.")).toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1500);
    });

    expect(connectSessionSocketMock).toHaveBeenCalledTimes(2);
  });

  it("retries session list loading when retry is clicked without an active session", async () => {
    listSessionsMock
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce([
        {
          sessionId: "sess_030",
          title: "recovered list session",
          status: "WAITING",
          lastTurnId: "turn_030",
          updatedAt: "2026-04-13T10:10:00Z",
          preview: "recovered preview",
        },
      ]);
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_030",
      title: "recovered list session",
      status: "WAITING",
      lastTurnId: "turn_030",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to load session list.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(listSessionsMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("recovered list session")).toBeInTheDocument();
  });

  it("retries active session bootstrap when retry is clicked", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_040",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_040",
          title: "retry bootstrap",
          status: "WAITING",
          updatedAt: "2026-04-13T10:20:00Z",
          preview: "retry bootstrap preview",
        },
      ],
    });
    getSessionMock
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce({
        sessionId: "sess_040",
        title: "retry bootstrap",
        status: "WAITING",
        lastTurnId: "turn_040",
        turns: [],
        runs: [],
        externalActivities: [],
        externalMessages: [],
        externalTasks: [],
      });

    render(<App />);

    expect(await screen.findByRole("alert")).toHaveTextContent("Failed to load session details.");
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(getSessionMock).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(connectSessionSocketMock).toHaveBeenCalledTimes(2));
  });

  it("restores recent sessions and focused run from local storage on load", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_002",
      focusedRunId: "run_201",
      recentSessions: [
        {
          sessionId: "sess_002",
          title: "restored session",
          status: "RUNNING",
          updatedAt: "2026-04-12T12:00:00Z",
          preview: "restored preview",
        },
      ],
    });

    render(<App />);

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_002" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          sessionId: "sess_002",
          threadId: "sess_002",
          title: "restored session",
          currentRunId: "run_201",
          turnId: "turn_201",
          lastTurnId: "turn_201",
          turns: [
            {
              turnId: "turn_201",
              sessionId: "sess_002",
              primaryRunId: "run_201",
              currentRunId: "run_201",
              latestRunId: "run_201",
              latestAgentId: "reviewer",
              displayState: "running",
              latestEventType: "run.started",
              latestMessagePreview: "restored preview",
              eventCount: 1,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_201"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: false,
              updatedAt: "2026-04-12T12:00:00Z",
            },
          ],
          runs: [
            {
              runId: "run_201",
              turnId: "turn_201",
              sessionId: "sess_002",
              agentId: "reviewer",
              displayState: "running",
              latestEventType: "run.started",
              latestMessageId: "msg_201",
              latestMessageText: "restored preview",
              summary: "",
              externalTaskIds: [],
              eventCount: 1,
              updatedAt: "2026-04-12T12:00:00Z",
            },
          ],
          externalTasks: [],
          externalMessages: [],
        }),
      });
    });

    expect(await screen.findByText("restored session")).toBeInTheDocument();
    expect(screen.getByText("run_201")).toBeInTheDocument();
  });

  it("loads server-side sessions and restores the latest one when local storage is empty", async () => {
    listSessionsMock.mockResolvedValueOnce([
      {
        sessionId: "sess_010",
        title: "server latest",
        status: "WAITING",
        lastTurnId: "turn_010",
        updatedAt: "2026-04-13T08:00:00Z",
        preview: "server preview",
      },
      {
        sessionId: "sess_009",
        title: "server older",
        status: "WAITING",
        lastTurnId: "turn_009",
        updatedAt: "2026-04-13T07:00:00Z",
        preview: "older preview",
      },
    ]);
    getSessionMock.mockImplementation(async (sessionId: string) => ({
      sessionId,
      title: sessionId === "sess_010" ? "server latest" : "server older",
      status: "WAITING",
      lastTurnId: sessionId === "sess_010" ? "turn_010" : "turn_009",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    }));

    render(<App />);

    await waitFor(() => expect(listSessionsMock).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_010" }),
      ),
    );
    expect(await screen.findByText("server latest")).toBeInTheDocument();
    expect(screen.getByText("server older")).toBeInTheDocument();
  });

  it("applies HTTP session bootstrap details before the websocket snapshot arrives", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_003",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_003",
          title: "stale title",
          status: "OPEN",
          updatedAt: "2026-04-12T12:30:00Z",
          preview: "stale preview",
        },
      ],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_003",
      title: "bootstrap title",
      status: "WAITING",
      lastTurnId: "turn_900",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [
        {
          externalTaskId: "ext_bootstrap",
          sessionId: "sess_003",
          turnId: "turn_900",
          runId: "run_bootstrap",
          adapter: "codex",
          remoteAgentName: "bootstrap-codex",
          remoteSessionId: "remote_bootstrap_session",
          remoteTaskId: "remote_bootstrap_task",
          state: "running",
          lastHeartbeatAt: "2026-04-12T12:31:00Z",
          resultSummary: "",
          metadata: {},
        },
      ],
    });

    render(<App />);

    expect(await screen.findByText("bootstrap title")).toBeInTheDocument();
    expect(screen.getAllByText("bootstrap-codex")).toHaveLength(3);
    expect(screen.getAllByText("Turn turn_900").length).toBeGreaterThan(0);
    expect(screen.getByText("run_bootstrap")).toBeInTheDocument();
    expect(screen.getAllByText("Waiting for websocket snapshot...").length).toBeGreaterThan(0);
  });

  it("uses aggregated HTTP session detail to restore real turn and run content before websocket snapshot", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_004",
      focusedRunId: null,
      recentSessions: [],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_004",
      title: "detail restored",
      status: "WAITING",
      lastTurnId: "turn_detail",
      turns: [
        {
          turnId: "turn_detail",
          sessionId: "sess_004",
          primaryRunId: "run_detail",
          currentRunId: "run_detail",
          latestRunId: "run_detail",
          latestAgentId: "default",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessagePreview: "detail summary",
          eventCount: 4,
          runCount: 1,
          activeRunCount: 0,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: [],
          completedRunIds: ["run_detail"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T12:40:00Z",
        },
      ],
      runs: [
        {
          runId: "run_detail",
          turnId: "turn_detail",
          sessionId: "sess_004",
          agentId: "default",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "msg_detail",
          latestMessageText: "detail thinking...",
          summary: "",
          externalTaskIds: [],
          eventCount: 4,
          updatedAt: "2026-04-12T12:40:00Z",
        },
      ],
      externalActivities: [
        {
          externalTaskId: "",
          sessionId: "sess_004",
          turnId: "turn_detail",
          runId: "run_detail",
          agentId: "default",
          activityId: "run_detail:external",
          kind: "run_status",
          status: "completed",
          title: "run_status",
          detail: "completed",
          startedAt: "2026-04-12T12:39:00Z",
          updatedAt: "2026-04-12T12:40:00Z",
        },
      ],
      externalMessages: [
        {
          externalTaskId: "",
          messageId: "msg_detail",
          sessionId: "sess_004",
          turnId: "turn_detail",
          runId: "run_detail",
          agentId: "default",
          content: "detail thinking...",
          started: true,
          completed: true,
          lastEventType: "run.external.message.end",
          lastSequence: 4,
          updatedAt: "2026-04-12T12:40:00Z",
        },
      ],
      externalTasks: [],
    });

    render(<App />);

    expect(await screen.findByText("detail restored")).toBeInTheDocument();
    expect(screen.getAllByText("Turn turn_detail").length).toBeGreaterThan(0);
    expect(screen.getByText("run_detail")).toBeInTheDocument();
    expect(screen.getAllByText("detail summary").length).toBeGreaterThan(0);
    expect(screen.getAllByText("detail thinking...").length).toBeGreaterThan(0);
    expect(screen.queryByText("Waiting for websocket snapshot...")).not.toBeInTheDocument();
  });

  it("loads focused run detail on demand and refreshes the inspector with detailed content", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_005",
      focusedRunId: null,
      recentSessions: [],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_005",
      title: "history session",
      status: "RUNNING",
      lastTurnId: "turn_current",
      turns: [
        {
          turnId: "turn_current",
          sessionId: "sess_005",
          primaryRunId: "run_current",
          currentRunId: "run_current",
          latestRunId: "run_current",
          latestAgentId: "default",
          displayState: "running",
          latestEventType: "run.started",
          latestMessagePreview: "当前在跑",
          eventCount: 2,
          runCount: 2,
          activeRunCount: 1,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: ["run_current"],
          completedRunIds: ["run_hist"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T15:00:00Z",
        },
      ],
      runs: [
        {
          runId: "run_current",
          turnId: "turn_current",
          sessionId: "sess_005",
          agentId: "default",
          displayState: "running",
          latestEventType: "run.started",
          latestMessageId: "msg_current",
          latestMessageText: "当前在跑",
          summary: "",
          externalTaskIds: [],
          eventCount: 2,
          updatedAt: "2026-04-12T15:00:00Z",
        },
        {
          runId: "run_hist",
          turnId: "turn_hist",
          sessionId: "sess_005",
          agentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "",
          latestMessageText: "",
          summary: "",
          externalTaskIds: [],
          eventCount: 1,
          updatedAt: "2026-04-12T14:00:00Z",
        },
      ],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });
    getRunDetailMock.mockResolvedValueOnce({
      sessionId: "sess_005",
      turnId: "turn_hist",
      turn: {
        turnId: "turn_hist",
        sessionId: "sess_005",
        primaryRunId: "run_hist",
        currentRunId: "run_hist",
        latestRunId: "run_hist",
        latestAgentId: "reviewer",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessagePreview: "历史摘要",
        eventCount: 8,
        runCount: 1,
        activeRunCount: 0,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: [],
        completedRunIds: ["run_hist"],
        failedRunIds: [],
        hasExternalWork: true,
        updatedAt: "2026-04-12T15:01:00Z",
      },
      run: {
        runId: "run_hist",
        turnId: "turn_hist",
        sessionId: "sess_005",
        agentId: "reviewer",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessageId: "msg_hist",
        latestMessageText: "done",
        summary: "历史摘要",
        externalTaskIds: ["ext_hist"],
        eventCount: 8,
        updatedAt: "2026-04-12T15:01:00Z",
      },
      externalActivities: [
        {
          externalTaskId: "ext_hist",
          sessionId: "sess_005",
          turnId: "turn_hist",
          runId: "run_hist",
          agentId: "reviewer",
          activityId: "run_hist:external",
          kind: "delegate",
          status: "completed",
          title: "delegate",
          detail: "completed",
          startedAt: "2026-04-12T15:00:30Z",
          updatedAt: "2026-04-12T15:01:00Z",
        },
      ],
      externalMessages: [
        {
          externalTaskId: "ext_hist",
          messageId: "msg_hist",
          sessionId: "sess_005",
          turnId: "turn_hist",
          runId: "run_hist",
          agentId: "reviewer",
          content: "历史详细正文",
          started: true,
          completed: true,
          lastEventType: "run.external.message.end",
          lastSequence: 8,
          updatedAt: "2026-04-12T15:01:00Z",
        },
      ],
      externalTasks: [
        {
          externalTaskId: "ext_hist",
          sessionId: "sess_005",
          turnId: "turn_hist",
          runId: "run_hist",
          adapter: "codex",
          remoteAgentName: "codex-reviewer",
          remoteSessionId: "remote_hist",
          remoteTaskId: "task_hist",
          state: "completed",
          lastHeartbeatAt: "2026-04-12T15:01:00Z",
          resultSummary: "历史任务完成",
          metadata: {},
        },
      ],
    });

    render(<App />);

    expect(await screen.findByText("history session")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /reviewercompletedWaiting for output\.\.\./ }));

    await waitFor(() =>
      expect(getRunDetailMock).toHaveBeenCalledWith("sess_005", "run_hist"),
    );
    expect((await screen.findAllByText("历史摘要")).length).toBeGreaterThan(0);
    expect(screen.getByText("历史详细正文")).toBeInTheDocument();
    expect(screen.getByText("delegate: completed")).toBeInTheDocument();
    expect(screen.getByText("codex-reviewer")).toBeInTheDocument();
    expect(screen.getByText("历史任务完成")).toBeInTheDocument();
  });

  it("retries focused run detail loading from the inspector after a failure", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_006",
      focusedRunId: null,
      recentSessions: [],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_006",
      title: "detail retry session",
      status: "RUNNING",
      lastTurnId: "turn_current",
      turns: [
        {
          turnId: "turn_current",
          sessionId: "sess_006",
          primaryRunId: "run_current",
          currentRunId: "run_current",
          latestRunId: "run_current",
          latestAgentId: "default",
          displayState: "running",
          latestEventType: "run.started",
          latestMessagePreview: "当前执行中",
          eventCount: 2,
          runCount: 2,
          activeRunCount: 1,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: ["run_current"],
          completedRunIds: ["run_hist"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T16:00:00Z",
        },
      ],
      runs: [
        {
          runId: "run_current",
          turnId: "turn_current",
          sessionId: "sess_006",
          agentId: "default",
          displayState: "running",
          latestEventType: "run.started",
          latestMessageId: "msg_current",
          latestMessageText: "当前执行中",
          summary: "",
          externalTaskIds: [],
          eventCount: 2,
          updatedAt: "2026-04-12T16:00:00Z",
        },
        {
          runId: "run_hist",
          turnId: "turn_hist",
          sessionId: "sess_006",
          agentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "",
          latestMessageText: "",
          summary: "",
          externalTaskIds: [],
          eventCount: 1,
          updatedAt: "2026-04-12T15:00:00Z",
        },
      ],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });
    getRunDetailMock
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce({
        sessionId: "sess_006",
        turnId: "turn_hist",
        turn: {
          turnId: "turn_hist",
          sessionId: "sess_006",
          primaryRunId: "run_hist",
          currentRunId: "run_hist",
          latestRunId: "run_hist",
          latestAgentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessagePreview: "已重试恢复",
          eventCount: 5,
          runCount: 1,
          activeRunCount: 0,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: [],
          completedRunIds: ["run_hist"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T16:01:00Z",
        },
        run: {
          runId: "run_hist",
          turnId: "turn_hist",
          sessionId: "sess_006",
          agentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "msg_hist",
          latestMessageText: "done",
          summary: "已重试恢复",
          externalTaskIds: [],
          eventCount: 5,
          updatedAt: "2026-04-12T16:01:00Z",
        },
        externalActivities: [],
        externalMessages: [],
        externalTasks: [],
      });

    render(<App />);

    expect(await screen.findByText("detail retry session")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /reviewercompletedWaiting for output\.\.\./ }));

    await waitFor(() =>
      expect(getRunDetailMock).toHaveBeenCalledWith("sess_006", "run_hist"),
    );
    expect(await screen.findByText("Failed to load run detail.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Retry Detail" }));

    await waitFor(() => expect(getRunDetailMock).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(screen.getAllByText("已重试恢复").length).toBeGreaterThan(0),
    );
  });

  it("shows recent turns in the conversation pane and focuses the selected turn run", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_007",
      focusedRunId: null,
      recentSessions: [],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_007",
      title: "turn history session",
      status: "RUNNING",
      lastTurnId: "turn_202",
      turns: [
        {
          turnId: "turn_201",
          sessionId: "sess_007",
          primaryRunId: "run_201",
          currentRunId: "run_201",
          latestRunId: "run_201",
          latestAgentId: "planner",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessagePreview: "历史规划已完成",
          eventCount: 3,
          runCount: 1,
          activeRunCount: 0,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: [],
          completedRunIds: ["run_201"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T14:00:00Z",
        },
        {
          turnId: "turn_202",
          sessionId: "sess_007",
          primaryRunId: "run_202",
          currentRunId: "run_202",
          latestRunId: "run_202",
          latestAgentId: "reviewer",
          displayState: "running",
          latestEventType: "run.started",
          latestMessagePreview: "当前在继续评审",
          eventCount: 2,
          runCount: 1,
          activeRunCount: 1,
          completedRunCount: 0,
          failedRunCount: 0,
          activeRunIds: ["run_202"],
          completedRunIds: [],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T15:00:00Z",
        },
      ],
      runs: [
        {
          runId: "run_201",
          turnId: "turn_201",
          sessionId: "sess_007",
          agentId: "planner",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessageId: "msg_201",
          latestMessageText: "历史规划已完成",
          summary: "历史规划已完成",
          externalTaskIds: [],
          eventCount: 3,
          updatedAt: "2026-04-12T14:00:00Z",
        },
        {
          runId: "run_202",
          turnId: "turn_202",
          sessionId: "sess_007",
          agentId: "reviewer",
          displayState: "running",
          latestEventType: "run.started",
          latestMessageId: "msg_202",
          latestMessageText: "当前在继续评审",
          summary: "",
          externalTaskIds: [],
          eventCount: 2,
          updatedAt: "2026-04-12T15:00:00Z",
        },
      ],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });
    getRunDetailMock.mockResolvedValueOnce({
      sessionId: "sess_007",
      turnId: "turn_201",
      turn: {
        turnId: "turn_201",
        sessionId: "sess_007",
        primaryRunId: "run_201",
        currentRunId: "run_201",
        latestRunId: "run_201",
        latestAgentId: "planner",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessagePreview: "历史规划已完成",
        eventCount: 3,
        runCount: 1,
        activeRunCount: 0,
        completedRunCount: 1,
        failedRunCount: 0,
        activeRunIds: [],
        completedRunIds: ["run_201"],
        failedRunIds: [],
        hasExternalWork: false,
        updatedAt: "2026-04-12T14:00:00Z",
      },
      run: {
        runId: "run_201",
        turnId: "turn_201",
        sessionId: "sess_007",
        agentId: "planner",
        displayState: "completed",
        latestEventType: "run.finished",
        latestMessageId: "msg_201",
        latestMessageText: "历史规划已完成",
        summary: "历史规划已完成",
        externalTaskIds: [],
        eventCount: 3,
        updatedAt: "2026-04-12T14:00:00Z",
      },
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    expect(await screen.findByText("turn history session")).toBeInTheDocument();
    expect(screen.getByText("Recent Turns")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Turn turn_201/ }));

    await waitFor(() =>
      expect(getRunDetailMock).toHaveBeenCalledWith("sess_007", "run_201"),
    );
    await waitFor(() =>
      expect(screen.getAllByText("历史规划已完成").length).toBeGreaterThan(0),
    );
  });

  it("sends continue_turn from Add Hint when the current turn is waiting for input", async () => {
    vi.stubGlobal("prompt", vi.fn().mockReturnValue("请补充错误边界和回退策略"));

    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "先启动一个等待输入的回合" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "WAITING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "waiting_input",
              latestEventType: "turn.waiting",
              latestMessagePreview: "需要更多约束",
              eventCount: 3,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:00:00Z",
            },
          ],
        }),
      });
    });

    const addHintButton = await screen.findByRole("button", { name: "Add Hint" });
    expect(addHintButton).toBeEnabled();

    fireEvent.click(addHintButton);

    expect(await screen.findByRole("button", { name: "Adding Hint..." })).toBeDisabled();
    expect(globalThis.prompt).toHaveBeenCalled();
    expect(sendContinueTurnMock).toHaveBeenCalledWith({
      message: "请补充错误边界和回退策略",
      agentId: "default",
      initiatorType: "user",
    });

    await act(async () => {
      onMessage?.({
        type: "command.accepted",
        command: "continue_turn",
        session_id: "sess_001",
      });
    });

    await waitFor(() => expect(screen.getByRole("button", { name: "Add Hint" })).toBeEnabled());
  });

  it("shows pending feedback while continuing a waiting turn from the composer", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_001",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_001",
          title: "waiting session",
          status: "WAITING",
          updatedAt: "2026-04-13T10:00:00Z",
          preview: "waiting preview",
        },
      ],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_001",
      title: "waiting session",
      status: "WAITING",
      lastTurnId: "turn_001",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "WAITING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "waiting_input",
              latestEventType: "turn.waiting",
              latestMessagePreview: "继续补充信息",
              eventCount: 3,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:00:00Z",
            },
          ],
        }),
      });
    });

    fireEvent.change(screen.getByPlaceholderText("当前任务在等待你的补充说明..."), {
      target: { value: "这里补充一条新的人工说明" },
    });
    const continueButton = screen
      .getAllByRole("button", { name: "Continue" })
      .find((button) => !button.hasAttribute("disabled"));
    expect(continueButton).toBeDefined();
    fireEvent.click(continueButton as HTMLButtonElement);

    expect(await screen.findByRole("button", { name: "Continuing..." })).toBeDisabled();
    expect(sendContinueTurnMock).toHaveBeenCalledWith({
      message: "这里补充一条新的人工说明",
      agentId: "default",
      initiatorType: "user",
    });

    await act(async () => {
      onMessage?.({
        type: "command.accepted",
        command: "continue_turn",
        session_id: "sess_001",
      });
    });

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Continuing..." })).not.toBeInTheDocument(),
    );
    await waitFor(() => {
      const enabledContinue = screen
        .getAllByRole("button", { name: "Continue" })
        .find((button) => !button.hasAttribute("disabled"));
      expect(enabledContinue).toBeDefined();
    });
  });

  it("shows inline feedback when continue_turn is rejected", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_001",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_001",
          title: "waiting session",
          status: "WAITING",
          updatedAt: "2026-04-13T10:00:00Z",
          preview: "waiting preview",
        },
      ],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_001",
      title: "waiting session",
      status: "WAITING",
      lastTurnId: "turn_001",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "WAITING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "waiting_input",
              latestEventType: "turn.waiting",
              latestMessagePreview: "继续补充信息",
              eventCount: 3,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:00:00Z",
            },
          ],
        }),
      });
    });

    fireEvent.change(screen.getByPlaceholderText("当前任务在等待你的补充说明..."), {
      target: { value: "这里补充一条新的人工说明" },
    });
    const continueButton = screen
      .getAllByRole("button", { name: "Continue" })
      .find((button) => !button.hasAttribute("disabled"));
    expect(continueButton).toBeDefined();
    fireEvent.click(continueButton as HTMLButtonElement);

    await act(async () => {
      onMessage?.({
        type: "command.error",
        error: "continue rejected",
      });
    });

    expect(await screen.findByText("Failed to continue turn.")).toBeInTheDocument();
  });

  it("does not enter continue mode for auth_required turns", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_001",
      focusedRunId: null,
      recentSessions: [
        {
          sessionId: "sess_001",
          title: "auth session",
          status: "WAITING",
          updatedAt: "2026-04-13T10:00:00Z",
          preview: "auth preview",
        },
      ],
    });
    getSessionMock.mockResolvedValueOnce({
      sessionId: "sess_001",
      title: "auth session",
      status: "WAITING",
      lastTurnId: "turn_001",
      turns: [],
      runs: [],
      externalActivities: [],
      externalMessages: [],
      externalTasks: [],
    });

    render(<App />);

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "WAITING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "auth_required",
              latestEventType: "run.status",
              latestMessagePreview: "需要审批后继续",
              eventCount: 3,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:00:00Z",
            },
          ],
        }),
      });
    });

    expect(screen.getByPlaceholderText("给 Axon 发送下一条任务说明...")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add Hint" })).toBeDisabled();
  });

  it("sends stop_turn for the current turn from the inspector", async () => {
    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "先启动一个可以停止的回合" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "RUNNING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "external_running",
              latestEventType: "run.external.activity",
              latestMessagePreview: "正在执行外部协作",
              eventCount: 4,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:02:00Z",
            },
          ],
        }),
      });
    });

    const stopButton = await screen.findByRole("button", { name: "Stop" });
    expect(stopButton).toBeEnabled();

    fireEvent.click(stopButton);

    expect(await screen.findByRole("button", { name: "Stopping..." })).toBeDisabled();
    expect(sendStopTurnMock).toHaveBeenCalledWith({
      turnId: "turn_001",
    });

    await act(async () => {
      onMessage?.({
        type: "command.accepted",
        command: "stop_turn",
        session_id: "sess_001",
      });
    });

    await waitFor(() => expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled());
  });

  it("shows inline feedback when stop_turn is rejected", async () => {
    render(<App />);

    fireEvent.change(screen.getByPlaceholderText("给 Axon 发送下一条任务说明..."), {
      target: { value: "先启动一个可以停止的回合" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_001" }),
      ),
    );

    await act(async () => {
      onMessage?.({
        type: "STATE_SNAPSHOT",
        snapshot: makeSnapshot({
          status: "RUNNING",
          turns: [
            {
              turnId: "turn_001",
              sessionId: "sess_001",
              primaryRunId: "run_001",
              currentRunId: "run_001",
              latestRunId: "run_001",
              latestAgentId: "default",
              displayState: "external_running",
              latestEventType: "run.external.activity",
              latestMessagePreview: "正在执行外部协作",
              eventCount: 4,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_001"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: true,
              updatedAt: "2026-04-12T11:02:00Z",
            },
          ],
        }),
      });
    });

    fireEvent.click(await screen.findByRole("button", { name: "Stop" }));

    await act(async () => {
      onMessage?.({
        type: "command.error",
        error: "stop rejected",
      });
    });

    expect(await screen.findByText("Failed to stop turn.")).toBeInTheDocument();
  });

  it("loads pending HITL tickets and can respond from the workbench", async () => {
    const respondDeferred = deferred<void>();
    listHITLTicketsMock
      .mockResolvedValueOnce([
        {
          id: "hitl-001",
          agentId: "reviewer",
          taskId: "task_001",
          sessionId: "sess_001",
          turnId: "turn_001",
          runId: "run_001",
          reason: "need approval",
          context: "请确认是否继续执行危险工具调用",
          status: "waiting",
          createdAt: "2026-04-13T06:00:00Z",
        },
      ])
      .mockResolvedValueOnce([]);
    respondHITLTicketMock.mockReturnValueOnce(respondDeferred.promise);

    render(<App />);

    expect(await screen.findByText("need approval")).toBeInTheDocument();
    expect(screen.getByText("请确认是否继续执行危险工具调用")).toBeInTheDocument();
    expect(screen.getByText("Session sess_001")).toBeInTheDocument();
    expect(screen.getByText("Turn turn_001")).toBeInTheDocument();
    expect(screen.getByText("Run run_001")).toBeInTheDocument();

    fireEvent.change(
      screen.getByPlaceholderText("给这个等待中的 ticket 一条人工指令..."),
      { target: { value: "继续执行，但不要写入生产环境" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Respond" }));

    expect(await screen.findByRole("button", { name: "Responding..." })).toBeDisabled();

    await waitFor(() =>
      expect(respondHITLTicketMock).toHaveBeenCalledWith(
        "hitl-001",
        "继续执行，但不要写入生产环境",
      ),
    );

    await act(async () => {
      respondDeferred.resolve();
    });

    await waitFor(() => expect(listHITLTicketsMock).toHaveBeenCalledTimes(2));
  });

  it("shows inline feedback when HITL respond fails", async () => {
    respondHITLTicketMock.mockRejectedValueOnce(new Error("respond failed"));
    listHITLTicketsMock.mockResolvedValueOnce([
      {
        id: "hitl-010",
        agentId: "reviewer",
        taskId: "task_010",
        sessionId: "sess_010",
        turnId: "turn_010",
        runId: "run_010",
        reason: "need approval",
        context: "需要失败反馈",
        status: "waiting",
        createdAt: "2026-04-13T06:00:00Z",
      },
    ]);

    render(<App />);

    fireEvent.change(
      await screen.findByPlaceholderText("给这个等待中的 ticket 一条人工指令..."),
      { target: { value: "继续执行" } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Respond" }));

    expect(await screen.findByText("Failed to respond to HITL ticket.")).toBeInTheDocument();
  });

  it("dismisses a pending HITL ticket from the workbench", async () => {
    const dismissDeferred = deferred<void>();
    listHITLTicketsMock
      .mockResolvedValueOnce([
        {
          id: "hitl-002",
          agentId: "reviewer",
          taskId: "task_002",
          sessionId: "sess_002",
          turnId: "turn_002",
          runId: "run_002",
          reason: "need manual decision",
          context: "请决定是否中止当前任务",
          status: "waiting",
          createdAt: "2026-04-13T06:10:00Z",
        },
      ])
      .mockResolvedValueOnce([]);
    dismissHITLTicketMock.mockReturnValueOnce(dismissDeferred.promise);

    render(<App />);

    expect(await screen.findByText("need manual decision")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    expect(await screen.findByRole("button", { name: "Dismissing..." })).toBeDisabled();

    await waitFor(() =>
      expect(dismissHITLTicketMock).toHaveBeenCalledWith("hitl-002"),
    );

    await act(async () => {
      dismissDeferred.resolve();
    });

    await waitFor(() => expect(listHITLTicketsMock).toHaveBeenCalledTimes(2));
  });

  it("opens ticket context and switches the workbench to the linked session and run", async () => {
    storage["axon.web.workbench"] = JSON.stringify({
      currentSessionId: "sess_001",
      focusedRunId: "run_001",
      recentSessions: [
        {
          sessionId: "sess_001",
          title: "current session",
          status: "RUNNING",
          updatedAt: "2026-04-13T05:59:00Z",
          preview: "current preview",
        },
      ],
    });

    getSessionMock.mockImplementation(async (sessionId: string) => {
      if (sessionId === "sess_002") {
        return {
          sessionId: "sess_002",
          title: "linked session",
          status: "WAITING",
          lastTurnId: "turn_200",
          turns: [
            {
              turnId: "turn_200",
              sessionId: "sess_002",
              primaryRunId: "run_200",
              currentRunId: "run_200",
              latestRunId: "run_200",
              latestAgentId: "reviewer",
              displayState: "waiting",
              latestEventType: "turn.waiting",
              latestMessagePreview: "需要人工确认",
              eventCount: 4,
              runCount: 1,
              activeRunCount: 1,
              completedRunCount: 0,
              failedRunCount: 0,
              activeRunIds: ["run_200"],
              completedRunIds: [],
              failedRunIds: [],
              hasExternalWork: false,
              updatedAt: "2026-04-13T06:00:00Z",
            },
          ],
          runs: [
            {
              runId: "run_200",
              turnId: "turn_200",
              sessionId: "sess_002",
              agentId: "reviewer",
              displayState: "waiting",
              latestEventType: "turn.waiting",
              latestMessageId: "",
              latestMessageText: "需要人工确认",
              summary: "",
              externalTaskIds: [],
              eventCount: 4,
              updatedAt: "2026-04-13T06:00:00Z",
            },
          ],
          externalActivities: [],
          externalMessages: [],
          externalTasks: [],
        };
      }

      return {
        sessionId,
        title: "current session",
        status: "OPEN",
        lastTurnId: "",
        turns: [],
        runs: [],
        externalActivities: [],
        externalMessages: [],
        externalTasks: [],
      };
    });

    listHITLTicketsMock.mockResolvedValue([
      {
        id: "hitl-003",
        agentId: "reviewer",
        taskId: "task_003",
        sessionId: "sess_002",
        turnId: "turn_200",
        runId: "run_200",
        reason: "open linked context",
        context: "请切到这个 ticket 对应的会话上下文",
        status: "waiting",
        createdAt: "2026-04-13T06:20:00Z",
      },
    ]);

    render(<App />);

    expect(await screen.findByText("open linked context")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open Context" }));

    await waitFor(() =>
      expect(connectSessionSocketMock).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: "sess_002" }),
      ),
    );
    expect(await screen.findByText("linked session")).toBeInTheDocument();
    await waitFor(() =>
      expect(getRunDetailMock).toHaveBeenCalledWith("sess_002", "run_200"),
    );
  });
});
