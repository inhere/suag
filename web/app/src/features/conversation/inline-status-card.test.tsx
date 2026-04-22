import { render, screen } from "@testing-library/react";
import { InlineStatusCard } from "./inline-status-card";

it("renders input required content", () => {
  render(<InlineStatusCard state="input_required" detail="请补充测试环境配置" />);

  expect(screen.getByRole("status")).toBeInTheDocument();
  expect(screen.getByText("Input required")).toBeInTheDocument();
  expect(screen.getByText("请补充测试环境配置")).toBeInTheDocument();
});

it("renders auth required content", () => {
  render(<InlineStatusCard state="auth_required" detail="等待团队负责人审批" />);

  expect(screen.getByText("Approval required")).toBeInTheDocument();
  expect(screen.getByText("等待团队负责人审批")).toBeInTheDocument();
});
