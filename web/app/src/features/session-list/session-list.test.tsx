import { render, screen } from "@testing-library/react";
import { SessionList } from "./session-list";

it("renders compact session items from recent known sessions with title, status, and preview", () => {
  render(
    <SessionList
      sessions={[
        {
          sessionId: "sess_001",
          title: "host runtime",
          status: "RUNNING",
          preview: "正在整理页面结构",
          updatedAt: "2m",
        },
      ]}
      currentSessionId="sess_001"
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
});
