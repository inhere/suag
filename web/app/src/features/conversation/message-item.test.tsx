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

it("renders ordered markdown steps as compact list items", () => {
  render(
    <MessageItem
      role="assistant"
      text={
        "### 方法一：修改设置\n\n1. 打开 VS Code 设置：按 `Ctrl + ,`\n\n2. 搜索 `openFoldersInNewWindow`\n\n3. 找到 **Window: Open Folders In New Window** 设置，将其改为 `on`"
      }
      timestamp="10:02"
    />,
  );

  const list = screen.getByRole("list");
  const items = within(list).getAllByRole("listitem");
  expect(items).toHaveLength(3);
  expect(items[0]).toHaveTextContent("打开 VS Code 设置：按 Ctrl + ,");
  expect(items[1]).toHaveTextContent("搜索 openFoldersInNewWindow");
  expect(items[2]).toHaveTextContent("Window: Open Folders In New Window 设置，将其改为 on");
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
