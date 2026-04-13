import { render, screen } from "@testing-library/react";
import { AppShell } from "./app-shell";

it("renders the three-column workbench shell", () => {
  render(<AppShell connectionStatus="connected" connectionStatusMessage="Session sess_001 connected." />);

  expect(screen.getByText("Sessions")).toBeInTheDocument();
  expect(screen.getByText("Conversation")).toBeInTheDocument();
  expect(screen.getByText("Run Inspector")).toBeInTheDocument();
  expect(screen.getByText("Session sess_001 connected.")).toBeInTheDocument();
});
