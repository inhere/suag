import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";
import type { MemoryCandidate } from "../../lib/host-types";
import { MemoryCandidatesPanel } from "./memory-candidates-panel";

function createCandidate(
  candidateId: string,
  target: string,
  content: string,
): MemoryCandidate {
  return {
    candidateId,
    sessionId: "sess_001",
    turnId: "turn_001",
    runId: "run_001",
    sourceRecapId: "recap_001",
    target,
    kind: "fact",
    content,
    score: 0.9,
    status: "pending",
    createdAt: "2026-04-17T10:00:00Z",
    metadata: {},
  };
}

it("shows MEMORY and USER group headings when both groups exist", () => {
  render(
    <MemoryCandidatesPanel
      candidates={[
        createCandidate("cand_memory_1", "MEMORY", "memory content"),
        createCandidate("cand_user_1", "USER", "user content"),
      ]}
    />,
  );

  expect(screen.getByRole("heading", { name: "MEMORY" })).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "USER" })).toBeInTheDocument();
});

it("shows empty message when candidate list is empty", () => {
  render(<MemoryCandidatesPanel candidates={[]} />);

  expect(screen.getByText("No pending memory candidates.")).toBeInTheDocument();
});

it("shows loading message when loading", () => {
  render(<MemoryCandidatesPanel candidates={[]} loading />);

  expect(screen.getByText("Loading memory candidates...")).toBeInTheDocument();
});

it("shows list error and retry button when errorMessage exists", () => {
  const onRetry = vi.fn();
  render(
    <MemoryCandidatesPanel
      candidates={[]}
      errorMessage="network error"
      onRetry={onRetry}
    />,
  );

  expect(screen.getByText("Failed to load memory candidates.")).toBeInTheDocument();
  const retryButton = screen.getByRole("button", { name: "Retry" });
  expect(retryButton).toBeInTheDocument();
  fireEvent.click(retryButton);
  expect(onRetry).toHaveBeenCalledTimes(1);
});

it("disables only matched candidate actions when pendingCandidateIds is set", () => {
  render(
    <MemoryCandidatesPanel
      candidates={[
        createCandidate("cand_1", "MEMORY", "first"),
        createCandidate("cand_2", "USER", "second"),
      ]}
      pendingCandidateIds={["cand_1"]}
    />,
  );

  const firstCard = screen.getByText("first").closest("article");
  const secondCard = screen.getByText("second").closest("article");

  expect(firstCard).not.toBeNull();
  expect(secondCard).not.toBeNull();
  expect(within(firstCard as HTMLElement).getByRole("button", { name: "Accept" })).toBeDisabled();
  expect(within(firstCard as HTMLElement).getByRole("button", { name: "Reject" })).toBeDisabled();
  expect(
    within(secondCard as HTMLElement).getByRole("button", { name: "Accept" }),
  ).not.toBeDisabled();
  expect(
    within(secondCard as HTMLElement).getByRole("button", { name: "Reject" }),
  ).not.toBeDisabled();
});
