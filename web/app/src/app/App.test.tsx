import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { SessionSocketMessage, StateSnapshotV0 } from "../lib/host-types";

const createSessionMock = vi.fn();
const getSessionMock = vi.fn();
const startTurnMock = vi.fn();
const sendContinueTurnMock = vi.fn();
const closeSocketMock = vi.fn();
const connectSessionSocketMock = vi.fn();

vi.mock("../lib/session-client", () => ({
  createSession: (...args: unknown[]) => createSessionMock(...args),
  getSession: (...args: unknown[]) => getSessionMock(...args),
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

  beforeEach(() => {
    onMessage = null;
    storage = {};
    createSessionMock.mockReset();
    getSessionMock.mockReset();
    startTurnMock.mockReset();
    sendContinueTurnMock.mockReset();
    closeSocketMock.mockReset();
    connectSessionSocketMock.mockReset();

    createSessionMock.mockResolvedValue({
      sessionId: "sess_001",
      title: "host runtime",
      status: "OPEN",
    });
    getSessionMock.mockResolvedValue({
      sessionId: "sess_001",
      title: "host runtime",
      status: "OPEN",
      lastTurnId: "",
      externalTasks: [],
    });
    startTurnMock.mockResolvedValue({
      sessionId: "sess_001",
      turnId: "turn_001",
      runId: "run_001",
    });
    connectSessionSocketMock.mockImplementation((options: { onMessage: (message: SessionSocketMessage) => void }) => {
      onMessage = options.onMessage;
      return {
        close: closeSocketMock,
        sendContinueTurn: sendContinueTurnMock,
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
    expect(await screen.findByText("thinking...")).toBeInTheDocument();
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
});
