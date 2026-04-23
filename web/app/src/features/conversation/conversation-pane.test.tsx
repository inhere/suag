import { render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import { ConversationPane } from "./conversation-pane";

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
      waitingForInput={false}
      headerTitle="Chat"
      headerMeta="Turn turn_012 · external running"
      summaryStatus="正在生成页面草图"
      summaryTone="running"
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
      waitingForInput
      headerMeta="Turn turn_012 · input required"
      summaryStatus="请补充部署环境变量"
      summaryTone="waiting"
      inlineStatus={{ state: "input_required", detail: "请补充部署环境变量" }}
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
      waitingForInput
      headerMeta="Turn turn_012 · waiting input"
      summaryStatus="请补充部署环境变量"
      summaryTone="waiting"
      inlineStatus={{ state: "input_required", detail: "请补充部署环境变量" }}
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
      waitingForInput={false}
      headerMeta="Turn turn_012 · auth required"
      summaryStatus="等待管理员审批"
      summaryTone="attention"
      inlineStatus={{ state: "auth_required", detail: "等待管理员审批" }}
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
      waitingForInput={false}
      headerMeta="Turn turn_012 · external running"
      summaryStatus="正在处理任务"
      summaryTone="running"
      onSubmit={vi.fn()}
    />,
  );

  expect(screen.queryByText("Input required")).not.toBeInTheDocument();
  expect(screen.queryByText("Approval required")).not.toBeInTheDocument();
});
