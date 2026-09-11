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

import type { Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { noRouteFilters } from "~/components/networks/routes-model.ts";
import type { RouteFilterState } from "~/components/networks/routes-model.ts";
import { RoutesTab } from "~/components/networks/routes-tab.tsx";

const stamp = "2026-01-01T12:00:00Z";

const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scoped: false,
  scopes: [],
  permissions: { "devices:routes": true, "devices:core": true },
};

const owner = {
  approved: true,
  approvedAt: stamp,
  createdAt: stamp,
  displayName: "Ada",
  email: "",
  id: "1",
  name: "ada",
  profilePicUrl: "",
  provider: "",
  providerId: "",
  role: "member",
};

interface NodeSpec {
  readonly available?: string[];
  readonly approved?: string[];
  readonly served?: string[];
  readonly online?: boolean;
}

function node(id: string, spec: NodeSpec = {}): Node {
  return {
    appConnector: false,
    remoteConfig: false,
    sshServer: false,
    approved: true,
    approvedAt: stamp,
    announcedServices: [],
    approvedRoutes: spec.approved ?? [],
    approvedServices: [],
    availableRoutes: spec.available ?? [],
    clientWarnings: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    funnelEnabled: false,
    clientVersion: "",
    os: "",
    osVersion: "",
    updateAvailable: false,
    ephemeral: false,
    id,
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: `host-${id}`,
    nodeKey: "nodekey:1",
    online: spec.online ?? true,
    preAuthKey: {
      aclTags: [],
      createdAt: null,
      ephemeral: false,
      expiration: null,
      id: "0",
      key: "",
      preauthorized: false,
      reusable: false,
      used: false,
      user: owner,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    suspended: false,
    suspendedAt: null,
    subnetRoutes: spec.served ?? [],
    tags: [],
    user: owner,
  };
}

/** The page around the table: it owns the search and the chips, as the route does. */
function Page({ nodes }: { readonly nodes: readonly Node[] }): ReactElement {
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<RouteFilterState>(noRouteFilters);

  return (
    <RoutesTab
      me={operator}
      nodes={nodes}
      networks={[]}
      search={search}
      filters={filters}
      onSearchChange={setSearch}
      onFiltersChange={setFilters}
    />
  );
}

function app(nodes: readonly Node[]): ReactElement {
  const rootRoute = createRootRoute({ component: () => <Page nodes={nodes} /> });
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

const pair = [
  node("1", { available: ["10.0.0.0/24"], approved: ["10.0.0.0/24"], served: ["10.0.0.0/24"] }),
  node("2", { available: ["10.0.0.0/24"], online: false }),
];

describe(RoutesTab, () => {
  it("folds a prefix two machines advertise into one row and unfolds it on the caret", async () => {
    const screen = await render(app(pair));

    await expect.element(screen.getByRole("cell", { name: /2 machines/u })).toBeVisible();
    await expect.element(screen.getByRole("cell", { name: "1 pending" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "machine-1" })).not.toBeInTheDocument();

    await screen.getByRole("button", { name: "Show the machines advertising 10.0.0.0/24" }).click();

    await expect
      .element(screen.getByRole("link", { name: "machine-1" }))
      .toHaveAttribute("href", "/machines/1");
    await expect.element(screen.getByRole("link", { name: "machine-2" })).toBeVisible();
    await expect.element(screen.getByText("Primary")).toBeVisible();
    await expect.element(screen.getByText("Offline")).toBeVisible();
  });

  it("leaves a prefix only one machine advertises a flat row", async () => {
    const screen = await render(app([node("1", { available: ["10.9.0.0/24"] })]));

    await expect.element(screen.getByRole("link", { name: "machine-1" })).toBeVisible();
    await expect.element(screen.getByRole("cell", { name: "Pending" })).toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: /^Show the machines/u }))
      .not.toBeInTheDocument();
  });

  it("narrows to the prefixes with something waiting", async () => {
    const nodes = [...pair, node("3", { available: ["10.9.0.0/24"], approved: ["10.9.0.0/24"] })];
    const screen = await render(app(nodes));

    await expect.element(screen.getByText("Showing 2 of 2 routes · 1 pending")).toBeVisible();

    await screen.getByRole("button", { name: /^Pending/u }).click();

    await expect.element(screen.getByText("Showing 1 of 2 routes · 1 pending")).toBeVisible();
    await expect.element(screen.getByText("10.9.0.0/24")).not.toBeInTheDocument();
  });

  it("keeps a partly stale prefix advertised and counts the machines that stopped", async () => {
    const nodes = [
      node("1", { available: ["10.0.0.0/24"], approved: ["10.0.0.0/24"] }),
      node("2", { approved: ["10.0.0.0/24"] }),
    ];
    const screen = await render(app(nodes));

    await expect.element(screen.getByRole("cell", { name: /2 machines/u })).toBeVisible();
    await expect.element(screen.getByText("1 stale")).toBeVisible();
    await expect.element(screen.getByRole("cell", { name: "Approved" })).toBeVisible();
    await expect.element(screen.getByText("No longer advertised")).not.toBeInTheDocument();
  });

  it("reads as no longer advertised once every machine stopped", async () => {
    const nodes = [
      node("1", { approved: ["10.0.0.0/24"] }),
      node("2", { approved: ["10.0.0.0/24"] }),
    ];
    const screen = await render(app(nodes));

    await expect.element(screen.getByText("No longer advertised")).toBeVisible();
    await expect.element(screen.getByText("1 stale")).not.toBeInTheDocument();
  });

  it("asks before revoking a route the machine still advertises", async () => {
    const approved = [node("1", { available: ["10.9.0.0/24"], approved: ["10.9.0.0/24"] })];
    const screen = await render(app(approved));

    await screen.getByRole("button", { name: "Revoke" }).click();

    await expect.element(screen.getByText("Revoke this route?")).toBeVisible();
    await expect
      .element(screen.getByText(/machine-1 stops carrying 10\.9\.0\.0\/24/u))
      .toBeVisible();
  });

  it("calls it Reject once the machine stopped advertising the route", async () => {
    const screen = await render(app([node("1", { approved: ["10.9.0.0/24"] })]));

    await screen.getByRole("button", { name: "Reject" }).click();

    await expect.element(screen.getByText("Reject this route?")).toBeVisible();
    await expect
      .element(screen.getByText(/machine-1 no longer advertises 10\.9\.0\.0\/24/u))
      .toBeVisible();
  });

  it("asks once before approving a prefix on every machine still waiting", async () => {
    const waiting = [
      node("1", { available: ["10.0.0.0/24"] }),
      node("2", { available: ["10.0.0.0/24"] }),
    ];
    const screen = await render(app(waiting));

    await screen.getByRole("button", { name: "Approve all" }).click();

    await expect.element(screen.getByText("Approve 2 machines?")).toBeVisible();
    await expect.element(screen.getByText(/10\.0\.0\.0\/24 start reaching/u)).toBeVisible();
  });
});
