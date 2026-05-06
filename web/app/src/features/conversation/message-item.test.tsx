import { render, screen, within } from "@testing-library/react";
import { MessageItem } from "./message-item";

it("renders assistant replies as markdown", () => {
  render(
    <MessageItem
      role="assistant"
      text={"## 工具列表\n\n- `file_read`\n- **file_write**\n\n```bash\npnpm test\n```"}
      timestamp="10:01"
    />,
  );

  expect(screen.getByRole("heading", { name: "工具列表", level: 2 })).toBeInTheDocument();
  expect(screen.getByRole("list")).toBeInTheDocument();
  expect(screen.getByText("file_read")).toBeInTheDocument();
  expect(screen.getByText("file_write")).toBeInTheDocument();
  expect(screen.getByText("pnpm test")).toBeInTheDocument();
});

it("keeps user messages as plain text instead of markdown", () => {
  render(
    <MessageItem
      role="user"
      text={"## 不应该变成标题\n\n- 不应该变成列表"}
      timestamp="10:00"
    />,
  );

  const message = screen.getByText(/不应该变成标题/).closest(".conversation-message");
  expect(message).not.toBeNull();
  expect(within(message as HTMLElement).queryByRole("heading")).not.toBeInTheDocument();
  expect(within(message as HTMLElement).queryByRole("list")).not.toBeInTheDocument();
});
