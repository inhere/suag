import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { DebugDrawer } from "./debug-drawer";

it("renders raw events only when the drawer is open", () => {
  const { rerender } = render(
    <DebugDrawer open={false} events={[{ type: "run.started", sequence: 1 }]} onClose={vi.fn()} />,
  );

  expect(screen.queryByText("run.started")).not.toBeInTheDocument();

  rerender(
    <DebugDrawer open events={[{ type: "run.started", sequence: 1 }]} onClose={vi.fn()} />,
  );

  expect(screen.getByText(/"type": "run\.started"/)).toBeInTheDocument();
});
