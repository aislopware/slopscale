import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import { nodesById } from "~/components/access-graph/model.ts";
import { ReachPanel } from "~/components/access-graph/reach-panels.tsx";

const nodes: AccessGraphNode[] = [
  { id: "1", name: "alpha", user: "ada", tags: [], online: true, routes: [] },
  { id: "2", name: "beta", user: "", tags: ["tag:web"], online: false, routes: ["10.0.0.0/24"] },
];

const edge: AccessGraphEdge = {
  src: "1",
  dst: "2",
  ports: ["tcp:22", "tcp:443", "udp:53"],
  routes: ["10.0.0.0/24"],
  sshUsers: ["ada"],
  sshCheck: true,
  capabilities: ["tailscale.com/cap/ingress"],
};

function app(children: ReactNode): ReactElement {
  const rootRoute = createRootRoute({ component: () => <div>{children}</div> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/machines/$nodeId" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

function panel(edges: readonly AccessGraphEdge[]): ReactElement {
  return app(
    <ReachPanel
      title="Can reach"
      description="What alpha may open."
      empty="Reaches nothing"
      edges={edges}
      peer="dst"
      index={nodesById(nodes)}
    />,
  );
}

describe(ReachPanel, () => {
  it("counts the direction in its heading", async () => {
    const screen = await render(panel([edge]));

    await expect.element(screen.getByRole("heading", { name: "Can reach · 1" })).toBeVisible();
  });

  it("names the machine at the other end, with its tags and a link to it", async () => {
    const screen = await render(panel([edge]));

    await expect
      .element(screen.getByRole("link", { name: "beta" }))
      .toHaveAttribute("href", "/machines/2");
    await expect.element(screen.getByText("tag:web")).toBeVisible();
  });

  it("gathers the ports by protocol and spells out the rest of the edge", async () => {
    const screen = await render(panel([edge]));

    await expect.element(screen.getByText("tcp:22, 443")).toBeVisible();
    await expect.element(screen.getByText("udp:53")).toBeVisible();
    await expect.element(screen.getByText("Asks for a fresh sign-in")).toBeVisible();
    await expect.element(screen.getByText("tailscale.com/cap/ingress")).toBeVisible();
  });

  it("lists the first edges and opens the rest on request", async () => {
    const many = Array.from({ length: 30 }, (_, index) => ({
      ...edge,
      dst: String(index + 10),
    }));
    const screen = await render(panel(many));

    await expect.element(screen.getByRole("heading", { name: "Can reach · 30" })).toBeVisible();
    await expect.element(screen.getByText("#34")).toBeVisible();
    expect(screen.container.querySelectorAll("a[href^='/machines/']")).toHaveLength(25);

    await screen.getByRole("button", { name: "Show all 30" }).click();

    await expect.element(screen.getByText("#39")).toBeVisible();
  });

  it("says so when the policy opens nothing in this direction", async () => {
    const screen = await render(panel([]));

    await expect.element(screen.getByText("Reaches nothing")).toBeVisible();
  });
});
