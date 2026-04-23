import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { SessionList } from "./session-list";
import { formatSessionUpdatedAt } from "./session-list-item";

it("renders compact session items from recent known sessions with title, status, and preview", () => {
  render(
    <SessionList
      sessions={[
        {
          sessionId: "sess_001",
          title: "host runtime",
          status: "RUNNING",
          preview: "正在整理页面结构",
          updatedAt: "2026-04-13T10:11:12.345Z",
        },
      ]}
      currentSessionId="sess_001"
      connectionStatus="connected"
      statusMessage="Session sess_001 connected."
      filter="all"
      searchQuery=""
      onCreateSession={() => {}}
      onSelect={() => {}}
      onSearchChange={() => {}}
      onFilterChange={() => {}}
    />,
  );

  expect(screen.getByText("host runtime")).toBeInTheDocument();
  expect(screen.getByText(/正在整理页面结构/)).toBeInTheDocument();
  expect(screen.getByText("Session sess_001 connected.")).toBeInTheDocument();
  expect(screen.getByText(formatSessionUpdatedAt("2026-04-13T10:11:12.345Z"))).toBeInTheDocument();
});

it("keeps preview on one line and calls select without relying on reorder", () => {
  const onSelect = vi.fn();

  render(
    <SessionList
      sessions={[
        {
          sessionId: "sess_001",
          title: "older session",
          status: "WAITING",
          preview: "这是一条很长的 preview message，用来验证单行截断展示。",
          updatedAt: "2026-04-13T10:11:12.345Z",
        },
        {
          sessionId: "sess_002",
          title: "newer session",
          status: "RUNNING",
          preview: "another preview",
          updatedAt: "2026-04-13T11:11:12.345Z",
        },
      ]}
      currentSessionId="sess_002"
      connectionStatus="connected"
      statusMessage="Session sess_002 connected."
      filter="all"
      searchQuery=""
      onCreateSession={() => {}}
      onSelect={onSelect}
      onSearchChange={() => {}}
      onFilterChange={() => {}}
    />,
  );

  const preview = screen.getByText("这是一条很长的 preview message，用来验证单行截断展示。");
  expect(preview).toHaveAttribute("title", "这是一条很长的 preview message，用来验证单行截断展示。");

  fireEvent.click(screen.getByRole("button", { name: /older session/i }));
  expect(onSelect).toHaveBeenCalledWith("sess_001");
});
