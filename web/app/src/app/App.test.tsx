import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { SessionSocketMessage, StateSnapshotV0 } from "../lib/host-types";

const createSessionMock = vi.fn();
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

describe("App", () => {
  let onMessage: ((message: SessionSocketMessage) => void) | null;
  let storage: Record<string, string>;

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  beforeEach(() => {
    onMessage = null;
    storage = {};
    createSessionMock.mockReset();
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
    connectSessionSocketMock.mockImplementation((options: { onMessage: (message: SessionSocketMessage) => void }) => {
      onMessage = options.onMessage;
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
    await waitFor(() =>
      expect(storage["axon.web.workbench"]).toContain("\"currentSessionId\":\"sess_001\""),
    );
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
    expect(screen.getByText("Turn turn_900")).toBeInTheDocument();
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
    expect(screen.getByText("Turn turn_detail")).toBeInTheDocument();
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

    expect(globalThis.prompt).toHaveBeenCalled();
    expect(sendContinueTurnMock).toHaveBeenCalledWith({
      message: "请补充错误边界和回退策略",
      agentId: "default",
      initiatorType: "user",
    });
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

    expect(sendStopTurnMock).toHaveBeenCalledWith({
      turnId: "turn_001",
    });
  });

  it("loads pending HITL tickets and can respond from the workbench", async () => {
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

    await waitFor(() =>
      expect(respondHITLTicketMock).toHaveBeenCalledWith(
        "hitl-001",
        "继续执行，但不要写入生产环境",
      ),
    );
    await waitFor(() => expect(listHITLTicketsMock).toHaveBeenCalledTimes(2));
  });

  it("dismisses a pending HITL ticket from the workbench", async () => {
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

    render(<App />);

    expect(await screen.findByText("need manual decision")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    await waitFor(() =>
      expect(dismissHITLTicketMock).toHaveBeenCalledWith("hitl-002"),
    );
    await waitFor(() => expect(listHITLTicketsMock).toHaveBeenCalledTimes(2));
  });
});
