import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { machineTabOf } from "~/components/machines/header.tsx";
import { StatusBadge } from "~/components/machines/status-badge.tsx";

describe(StatusBadge, () => {
  it("draws a disconnected machine as the solid inverted badge", async () => {
    const screen = await render(<StatusBadge status="offline" />);

    await expect.element(screen.getByText("Disconnected")).toHaveClass("bg-kumo-badge-inverted");
  });

  it("keeps connected green and the states to act on tinted", async () => {
    const screen = await render(
      <>
        <StatusBadge status="online" />
        <StatusBadge status="pending" />
        <StatusBadge status="expired" />
        <StatusBadge status="suspended" />
      </>,
    );

    await expect.element(screen.getByText("Connected")).toHaveClass("bg-kumo-success-tint");
    await expect.element(screen.getByText("Needs approval")).toHaveClass("bg-kumo-warning-tint");
    await expect.element(screen.getByText("Key expired")).toHaveClass("bg-kumo-danger-tint");
    await expect.element(screen.getByText("Suspended")).toHaveClass("bg-kumo-danger-tint");
  });
});

describe(machineTabOf, () => {
  it("reads the tab from the address under the machine", () => {
    expect(machineTabOf("/console/machines/7")).toBe("overview");
    expect(machineTabOf("/console/machines/7/")).toBe("overview");
    expect(machineTabOf("/console/machines/7/routes")).toBe("routes");
    expect(machineTabOf("/console/machines/7/diagnostics")).toBe("diagnostics");
    expect(machineTabOf("/console/machines/7/settings")).toBe("settings");
  });
});
