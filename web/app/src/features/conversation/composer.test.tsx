import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { Composer } from "./composer";

it("submits with Enter and clears the input", () => {
  const onSubmit = vi.fn();
  render(<Composer waitingForInput={false} onSubmit={onSubmit} />);

  const input = screen.getByRole("textbox");
  fireEvent.change(input, { target: { value: "请继续执行" } });
  fireEvent.keyDown(input, { key: "Enter" });

  expect(onSubmit).toHaveBeenCalledWith("请继续执行");
  expect(input).toHaveValue("");
});

it("keeps Ctrl+Enter available for multiline editing", () => {
  const onSubmit = vi.fn();
  render(<Composer waitingForInput={false} onSubmit={onSubmit} />);

  const input = screen.getByRole("textbox");
  fireEvent.change(input, { target: { value: "第一行" } });
  fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });

  expect(onSubmit).not.toHaveBeenCalled();
  expect(input).toHaveValue("第一行");
});

it("shows the keyboard shortcut hint next to the send action", () => {
  render(<Composer waitingForInput={false} onSubmit={vi.fn()} />);

  expect(screen.getByText("Enter 发送 · Ctrl+Enter 换行")).toBeInTheDocument();
});
