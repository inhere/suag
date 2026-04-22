import { render, screen } from "@testing-library/react";
import { SummaryStrip } from "./summary-strip";

it("renders short status with running tone class", () => {
  render(<SummaryStrip status="正在执行任务" tone="running" />);

  const status = screen.getByRole("status");
  expect(status).toHaveTextContent("正在执行任务");
  expect(status).toHaveClass("summary-strip");
  expect(status).toHaveClass("summary-strip--running");
});

it("renders failed tone class", () => {
  render(<SummaryStrip status="任务失败，请重试" tone="failed" />);

  expect(screen.getByRole("status")).toHaveClass("summary-strip--failed");
});
