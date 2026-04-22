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
  expect(screen.getByText("Sessions")).toBeInTheDocument();
  expect(screen.getByRole("main", { name: /Chat panel/i })).toBeInTheDocument();
  const contextPanel = screen.getByRole("region", { name: /Context panel/i });
  expect(contextPanel).toBeInTheDocument();
  expect(screen.queryByText("Conversation")).not.toBeInTheDocument();
  expect(screen.queryByText("Run Inspector")).not.toBeInTheDocument();

  const pendingHitlHeading = screen.getByText("Pending HITL");
  const sessionSummaryHeading = screen.getByText("Session summary");
  const memoryCandidatesHeading = screen.getByText("Memory Candidates");
  const runDetailsHeading = screen.getByText("Run details");
  expect(pendingHitlHeading.compareDocumentPosition(sessionSummaryHeading)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(sessionSummaryHeading.compareDocumentPosition(memoryCandidatesHeading)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
  expect(memoryCandidatesHeading.compareDocumentPosition(runDetailsHeading)).toBe(
    Node.DOCUMENT_POSITION_FOLLOWING,
  );
});
