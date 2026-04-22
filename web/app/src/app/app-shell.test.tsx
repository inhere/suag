import { render, screen } from "@testing-library/react";
import { AppShell } from "./app-shell";

it("renders the chat-first shell with sessions, chat, and context columns", () => {
  render(
    <AppShell
      sessions={[]}
      messages={[]}
      hitlTickets={[]}
      memoryCandidates={[]}
      currentTurn={null}
      runs={[]}
    />,
  );

  expect(screen.getByRole("complementary", { name: /Sessions panel/i })).toBeInTheDocument();
  expect(screen.getByRole("main", { name: /Chat panel/i })).toBeInTheDocument();
  expect(screen.getByRole("region", { name: /Context panel/i })).toBeInTheDocument();
  expect(screen.queryByText("Conversation")).not.toBeInTheDocument();
  expect(screen.queryByText("Run Inspector")).not.toBeInTheDocument();
  expect(screen.getByText("Pending HITL")).toBeInTheDocument();
  expect(screen.getByText("Session summary")).toBeInTheDocument();
  expect(screen.getByText("Memory Candidates")).toBeInTheDocument();
  expect(screen.getByText("Run details")).toBeInTheDocument();
});
