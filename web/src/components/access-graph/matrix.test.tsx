import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import { AccessMatrix } from "~/components/access-graph/matrix.tsx";
import { buildMatrix } from "~/components/access-graph/model.ts";

const nodes: AccessGraphNode[] = [
  { id: "1", name: "alpha", user: "ada", tags: [], online: true, routes: [] },
  { id: "2", name: "beta", user: "", tags: ["tag:web"], online: false, routes: [] },
];

const edge: AccessGraphEdge = {
  src: "1",
  dst: "2",
  ports: ["tcp:22"],
  routes: [],
  sshUsers: ["*"],
  sshCheck: false,
  capabilities: [],
};

describe(AccessMatrix, () => {
  it("marks the pair the policy opens and says what it opens", async () => {
    const screen = await render(
      <AccessMatrix
        matrix={buildMatrix(nodes, [edge])}
        onPick={vi.fn<(nodeId: string) => void>()}
      />,
    );

    await expect
      .element(screen.getByLabelText("alpha reaches beta: tcp:22 · SSH as any login"))
      .toBeInTheDocument();
    expect(screen.container.querySelectorAll("tbody tr")).toHaveLength(2);
  });

  it("keeps one tab stop and walks the grid with the arrow keys", async () => {
    const screen = await render(
      <AccessMatrix
        matrix={buildMatrix(nodes, [edge])}
        onPick={vi.fn<(nodeId: string) => void>()}
      />,
    );

    expect(screen.container.querySelectorAll("[data-row]")).toHaveLength(4);
    expect(screen.container.querySelectorAll('[data-row][tabindex="0"]')).toHaveLength(1);

    await screen.getByLabelText("alpha, its own row").click();
    await userEvent.keyboard("{ArrowRight}");

    await expect
      .element(screen.getByLabelText("alpha reaches beta: tcp:22 · SSH as any login"))
      .toHaveFocus();
    expect(screen.container.querySelectorAll('[data-row][tabindex="0"]')).toHaveLength(1);
    expect(
      screen.container.querySelector<HTMLElement>('[data-row][tabindex="0"]')?.dataset["col"],
    ).toBe("1");
  });

  it("picks a machine from either axis", async () => {
    const onPick = vi.fn<(nodeId: string) => void>();
    const screen = await render(
      <AccessMatrix matrix={buildMatrix(nodes, [edge])} onPick={onPick} />,
    );

    await screen.getByRole("button", { name: "beta" }).first().click();

    expect(onPick).toHaveBeenCalledWith("2");
  });
});
