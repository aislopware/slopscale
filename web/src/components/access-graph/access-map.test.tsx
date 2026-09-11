import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import { AccessMapView } from "~/components/access-graph/access-map.tsx";
import { buildAccessMap } from "~/components/access-graph/model.ts";

const nodes: AccessGraphNode[] = [
  { id: "1", name: "alpha", user: "ada", tags: [], online: true, routes: [] },
  { id: "2", name: "alpha-2", user: "ada", tags: [], online: true, routes: [] },
  { id: "3", name: "beta", user: "", tags: ["tag:web"], online: false, routes: [] },
];

/** The data attributes of the focused cell, the way the grid places it. */
function active(): DOMStringMap {
  return document.activeElement instanceof HTMLElement ? document.activeElement.dataset : {};
}

function edge(src: string, dst: string, extra: Partial<AccessGraphEdge> = {}): AccessGraphEdge {
  return {
    src,
    dst,
    ports: [],
    routes: [],
    sshUsers: [],
    sshCheck: false,
    capabilities: [],
    ...extra,
  };
}

const edges = [
  edge("1", "2", { ports: ["*"] }),
  edge("2", "1", { ports: ["*"] }),
  edge("1", "3", { ports: ["tcp:22"], sshUsers: ["*"] }),
  edge("2", "3", { ports: ["tcp:22"], sshUsers: ["*"] }),
];

describe(AccessMapView, () => {
  it("draws one row per class and says in the cell what it opens", async () => {
    const screen = await render(
      <AccessMapView
        map={buildAccessMap(nodes, edges)}
        onPick={vi.fn<(nodeId: string) => void>()}
      />,
    );

    expect(screen.container.querySelectorAll("tbody tr")).toHaveLength(2);
    await expect
      .element(screen.getByLabelText("ada (alpha +1) reaches beta: tcp:22 · SSH as any login"))
      .toHaveTextContent("tcp:22 · SSH");
    await expect
      .element(screen.getByLabelText("ada (alpha +1) reaches ada (alpha +1): Every port"))
      .toHaveTextContent("All ports");
    await expect.element(screen.getByText("alpha +1").first()).toBeVisible();
  });

  it("keeps one tab stop and walks the grid with the arrow keys", async () => {
    const screen = await render(
      <AccessMapView
        map={buildAccessMap(nodes, edges)}
        onPick={vi.fn<(nodeId: string) => void>()}
      />,
    );
    const cells = screen.container.querySelectorAll<HTMLElement>("td [data-row]");

    expect([...cells].filter((cell) => cell.tabIndex === 0)).toHaveLength(1);

    cells[0]?.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(active()["col"]).toBe("1");

    await userEvent.keyboard("{ArrowDown}");
    expect(active()["row"]).toBe("1");

    await userEvent.keyboard("{Home}");
    expect(active()["col"]).toBe("0");
  });

  it("lists a class's machines under its header and picks one", async () => {
    const onPick = vi.fn<(nodeId: string) => void>();
    const screen = await render(
      <AccessMapView map={buildAccessMap(nodes, edges)} onPick={onPick} />,
    );

    await screen.getByText("tag:web").first().click();
    await screen.getByRole("button", { name: "beta", exact: true }).click();

    expect(onPick).toHaveBeenCalledWith("3");
  });
});
