import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { App, AppNode } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { AppsTab } from "~/components/apps/apps-tab.tsx";

/** The width of the content area beside the sidebar on a 1280px screen. */
const contentWidth = 932;

const stamp = "2026-01-01T12:00:00Z";

const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scopes: [],
  permissions: { policy_file: true },
};

function connector(id: string, pending: number): AppNode {
  return {
    connector: true,
    learnedRoutes: 12,
    name: `connector-${id}.example-tailnet.ts.net`,
    nodeId: id,
    online: true,
    pending,
  };
}

const apps: readonly App[] = [
  {
    id: "1",
    name: "crm",
    description: "The sales CRM behind the office firewall, reachable from the tailnet only",
    domains: ["crm.example.com", "*.crm.example.com", "crm-staging.example.com"],
    connectors: ["tag:connector", "tag:office-connector"],
    routes: ["10.0.0.0/24"],
    nodes: [connector("1", 3), connector("2", 0)],
    createdAt: stamp,
    updatedAt: stamp,
  },
  {
    id: "2",
    name: "wiki",
    description: "The engineering wiki",
    domains: ["wiki.example.com"],
    connectors: [],
    routes: [],
    nodes: [connector("3", 2), connector("4", 1)],
    createdAt: stamp,
    updatedAt: stamp,
  },
];

/** The page around the table, in the width the content area has beside the sidebar. */
function Page(): ReactElement {
  const [search, setSearch] = useState("");

  return (
    <div style={{ width: `${contentWidth}px` }}>
      <AppsTab me={operator} apps={apps} search={search} onSearchChange={setSearch} />
    </div>
  );
}

function page(): ReactElement {
  const rootRoute = createRootRoute({ component: Page });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/routes" }),
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

/** Whether the table's columns run past the panel, which is what makes it scroll sideways. */
function scrollsSideways(container: HTMLElement): boolean {
  const scroll = container.querySelector("table")?.parentElement ?? null;

  return scroll !== null && scroll.scrollWidth > scroll.clientWidth;
}

describe(AppsTab, () => {
  it("fits the content area of a 1280px screen without scrolling sideways", async () => {
    const screen = await render(page());

    // The header of a sortable column is its button.
    await expect.element(screen.getByRole("button", { name: "Pending" })).toBeVisible();

    // Learned routes is the one column that waits for a wider screen; Pending stays.
    await expect.element(screen.getByText("Learned routes")).not.toBeVisible();

    expect(scrollsSideways(screen.container)).toBe(false);
  });

  it("links the pending count to the routes waiting for approval", async () => {
    const screen = await render(page());
    const links = screen.getByRole("link");

    // One connector waiting names it as well; several can only turn the chip on.
    await expect
      .element(links.nth(0))
      .toHaveAttribute("href", "/routes?pending=true&q=connector-1.example-tailnet.ts.net");
    await expect.element(links.nth(1)).toHaveAttribute("href", "/routes?pending=true");
  });
});
