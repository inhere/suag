import { render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { ConversationPane } from "./conversation-pane";

function buildTurn(displayState: string, preview: string) {
  return {
    turnId: "turn_012",
    sessionId: "sess_001",
    primaryRunId: "run_010",
    currentRunId: "run_011",
    latestRunId: "run_011",
    latestAgentId: "default",
    displayState,
    latestEventType: "run.started",
    latestMessagePreview: preview,
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
  };
}

it("renders chat-first layout with summary strip and composer feedback", () => {
  render(
    <ConversationPane
      messages={[
        {
          id: "user-1",
          role: "user",
          text: "请整理当前页面结构",
          timestamp: "10:00",
        },
        {
          id: "assistant-1",
          role: "assistant",
          text: "已开始处理这个任务",
          timestamp: "10:01",
        },
      ]}
      currentTurn={buildTurn("external_running", "正在生成页面草图")}
      waitingForInput={false}
      submitPending
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.getByRole("heading", { name: "Chat" })).toBeInTheDocument();
  expect(screen.getByText("正在生成页面草图")).toBeInTheDocument();
  expect(screen.queryByText("Recent Turns")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Starting..." })).toBeInTheDocument();
  expect(screen.getByText("正在创建新 turn...")).toBeInTheDocument();
  expect(screen.getByRole("textbox")).toBeInTheDocument();
  expect(screen.getByText("10:00")).toBeInTheDocument();
  expect(screen.getByText("10:01")).toBeInTheDocument();
});

it("shows inline status card for input required and keeps composer error feedback", () => {
  render(
    <ConversationPane
      messages={[]}
      currentTurn={buildTurn("input_required", "请补充部署环境变量")}
      waitingForInput
      submitError="提交失败，请稍后重试。"
      onSubmit={vi.fn()}
    />,
  );

  const statusCard = screen.getByText("Input required").closest(".inline-status-card");
  expect(statusCard).not.toBeNull();
  expect(within(statusCard as HTMLElement).getByText("请补充部署环境变量")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Continue" })).toBeInTheDocument();
  expect(screen.getByText("提交失败，请稍后重试。")).toBeInTheDocument();
});

it("maps waiting input to the same inline guidance as input required", () => {
  render(
    <ConversationPane
      messages={[]}
      currentTurn={buildTurn("waiting_input", "请补充部署环境变量")}
      waitingForInput
      onSubmit={vi.fn()}
    />,
  );

  const statusCard = screen.getByText("Input required").closest(".inline-status-card");
  expect(statusCard).not.toBeNull();
  expect(within(statusCard as HTMLElement).getByText("请补充部署环境变量")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Continue" })).toBeInTheDocument();
});

it("shows inline status card for auth required only", () => {
  render(
    <ConversationPane
      messages={[]}
      currentTurn={buildTurn("auth_required", "等待管理员审批")}
      waitingForInput={false}
      onSubmit={vi.fn()}
    />,
  );

  const statusCard = screen.getByText("Approval required").closest(".inline-status-card");
  expect(statusCard).not.toBeNull();
  expect(within(statusCard as HTMLElement).getByText("等待管理员审批")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument();
});

it("does not show inline status card for running state", () => {
  render(
    <ConversationPane
      messages={[]}
      currentTurn={buildTurn("external_running", "正在处理任务")}
      waitingForInput={false}
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.queryByText("Input required")).not.toBeInTheDocument();
  expect(screen.queryByText("Approval required")).not.toBeInTheDocument();
});
