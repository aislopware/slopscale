import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { AccessGraphNode } from "~/api/schema.gen.ts";
import { MachinePicker } from "~/components/access-graph/machine-picker.tsx";

const nodes: AccessGraphNode[] = [
  { id: "2", name: "beta", user: "", tags: ["tag:web"], online: false, routes: [] },
  { id: "1", name: "alpha", user: "ada", tags: [], online: true, routes: [] },
];

describe(MachinePicker, () => {
  it("offers the machines in name order and reports the one chosen", async () => {
    const onValueChange = vi.fn<(nodeId: string) => void>();
    const screen = await render(
      <MachinePicker nodes={nodes} value="" onValueChange={onValueChange} />,
    );

    await screen.getByRole("combobox").click();
    await screen.getByRole("option", { name: /beta/v }).click();

    expect(onValueChange).toHaveBeenCalledWith("2");
  });

  it("says that no machine means the whole tailnet", async () => {
    const screen = await render(
      <MachinePicker nodes={nodes} value="" onValueChange={vi.fn<(nodeId: string) => void>()} />,
    );

    await expect.element(screen.getByPlaceholder("Every machine")).toBeVisible();
  });
});
