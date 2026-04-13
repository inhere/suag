import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { ConversationPane } from "./conversation-pane";

it("renders user messages, assistant summaries, and turn summary cards", () => {
  render(
    <ConversationPane
      messages={[
        { id: "user-1", role: "user", text: "请整理当前页面结构" },
        { id: "assistant-1", role: "assistant", text: "已开始处理这个任务" },
      ]}
      currentTurn={{
        turnId: "turn_012",
        sessionId: "sess_001",
        primaryRunId: "run_010",
        currentRunId: "run_011",
        latestRunId: "run_011",
        latestAgentId: "default",
        displayState: "external_running",
        latestEventType: "run.started",
        latestMessagePreview: "正在生成页面草图",
        eventCount: 4,
        runCount: 2,
        activeRunCount: 2,
        completedRunCount: 0,
        failedRunCount: 0,
        activeRunIds: ["run_010", "run_011"],
        completedRunIds: [],
        failedRunIds: [],
        hasExternalWork: true,
        updatedAt: "2026-04-12T10:00:00Z",
      }}
      recentTurns={[
        {
          turnId: "turn_012",
          sessionId: "sess_001",
          primaryRunId: "run_010",
          currentRunId: "run_011",
          latestRunId: "run_011",
          latestAgentId: "default",
          displayState: "external_running",
          latestEventType: "run.started",
          latestMessagePreview: "正在生成页面草图",
          eventCount: 4,
          runCount: 2,
          activeRunCount: 2,
          completedRunCount: 0,
          failedRunCount: 0,
          activeRunIds: ["run_010", "run_011"],
          completedRunIds: [],
          failedRunIds: [],
          hasExternalWork: true,
          updatedAt: "2026-04-12T10:00:00Z",
        },
        {
          turnId: "turn_011",
          sessionId: "sess_001",
          primaryRunId: "run_009",
          currentRunId: "run_009",
          latestRunId: "run_009",
          latestAgentId: "reviewer",
          displayState: "completed",
          latestEventType: "run.finished",
          latestMessagePreview: "已完成方案评审",
          eventCount: 3,
          runCount: 1,
          activeRunCount: 0,
          completedRunCount: 1,
          failedRunCount: 0,
          activeRunIds: [],
          completedRunIds: ["run_009"],
          failedRunIds: [],
          hasExternalWork: false,
          updatedAt: "2026-04-12T09:40:00Z",
        },
      ]}
      onSelectTurn={vi.fn()}
      waitingForInput={false}
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.getAllByText("Turn turn_012").length).toBeGreaterThan(0);
  expect(screen.getAllByText(/external_running/).length).toBeGreaterThan(0);
  expect(screen.getByText("Recent Turns")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /Turn turn_011/ })).toBeInTheDocument();
  expect(screen.getByRole("textbox")).toBeInTheDocument();
});
