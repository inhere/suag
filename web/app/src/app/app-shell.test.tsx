import { render, screen } from "@testing-library/react";
import { AppShell } from "./app-shell";

it("renders the three-column workbench shell", () => {
  render(
    <AppShell
      contextSessionLabel="Session sess_001"
      contextTurnLabel="Turn turn_001"
      contextRunLabel="Run run_001"
      contextConnectionLabel="Socket connected"
      contextConnectionTone="connected"
      connectionStatus="connected"
      connectionStatusMessage="Session sess_001 connected."
    />,
  );

  expect(screen.getByText("Sessions")).toBeInTheDocument();
  expect(screen.getByText("Conversation")).toBeInTheDocument();
  expect(screen.getByText("Run Inspector")).toBeInTheDocument();
  expect(screen.getByText("Session sess_001")).toBeInTheDocument();
  expect(screen.getByText("Turn turn_001")).toBeInTheDocument();
  expect(screen.getByText("Run run_001")).toBeInTheDocument();
  expect(screen.getByText("Socket connected")).toBeInTheDocument();
  expect(screen.getByText("Session sess_001 connected.")).toBeInTheDocument();
});
