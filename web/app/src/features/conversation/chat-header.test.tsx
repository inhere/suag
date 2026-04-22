import { render, screen } from "@testing-library/react";
import { ChatHeader } from "./chat-header";

it("renders title and meta text", () => {
  render(<ChatHeader title="Chat" meta="Turn turn_001 · running" />);

  expect(screen.getByRole("heading", { name: "Chat" })).toBeInTheDocument();
  expect(screen.getByText("Turn turn_001 · running")).toBeInTheDocument();
});

it("hides meta text when meta is empty", () => {
  render(<ChatHeader title="Chat" meta={null} />);

  expect(screen.getByRole("heading", { name: "Chat" })).toBeInTheDocument();
  expect(screen.queryByText(/Turn/)).not.toBeInTheDocument();
});
