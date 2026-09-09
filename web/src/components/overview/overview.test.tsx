import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { GetStarted } from "~/components/overview/get-started.tsx";
import { MetricTiles } from "~/components/overview/metric-tiles.tsx";
import { NeedsAttention } from "~/components/overview/needs-attention.tsx";

const admin: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scopes: [],
  permissions: {
    "devices:core": true,
    "devices:core:read": true,
    users: true,
    "users:read": true,
    auth_keys: true,
    "feature_settings:read": true,
  },
};

const reader: Me = { ...admin, role: "auditor", permissions: { "devices:core:read": true } };

const alice: User = {
  approved: true,
  approvedAt: "2026-01-01T00:00:00Z",
  createdAt: "2026-01-01T00:00:00Z",
  displayName: "Alice",
  email: "alice@example.com",
  id: "1",
  name: "alice",
  profilePicUrl: "",
  provider: "oidc",
  providerId: "alice",
  role: "admin",
};

const newcomer: User = {
  ...alice,
  approved: false,
  approvedAt: null,
  displayName: "Bob",
  email: "bob@example.com",
  id: "2",
  name: "bob",
  role: "member",
};

const laptop: Node = {
  appConnector: false,
  sshServer: false,
  approved: true,
  approvedAt: "2026-01-01T00:00:00Z",
  announcedServices: [],
  approvedRoutes: [],
  approvedServices: [],
  availableRoutes: [],
  createdAt: "2026-01-01T00:00:00Z",
  discoKey: "",
  expiry: null,
  givenName: "laptop-alice",
  globalExitNode: false,
  funnelEnabled: false,
  clientVersion: "",
  updateAvailable: false,
  ephemeral: false,
  clientWarnings: [],
  id: "1",
  ipAddresses: ["100.64.0.1"],
  lastSeen: new Date().toISOString(),
  machineKey: "",
  name: "laptop-alice",
  nodeKey: "",
  online: true,
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
    user: alice,
  },
  registerMethod: "REGISTER_METHOD_OIDC",
  sharedWith: [],
  suspended: false,
  suspendedAt: null,
  subnetRoutes: [],
  tags: [],
  user: alice,
};

const gateway: Node = {
  ...laptop,
  approved: false,
  approvedAt: null,
  announcedServices: [],
  approvedRoutes: ["0.0.0.0/0"],
  approvedServices: [],
  availableRoutes: ["0.0.0.0/0"],
  givenName: "pi-gateway",
  id: "2",
  ipAddresses: ["100.64.0.2"],
  name: "pi-gateway",
  online: false,
};

/** The overview links to other pages, so every piece of it needs a router around it. */
function app(children: ReactNode): ReactElement {
  const rootRoute = createRootRoute({ component: () => <div>{children}</div> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/machines" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/machines/$nodeId" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/users" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/settings" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/settings/tailnet" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(MetricTiles, () => {
  it("counts machines, approvals, users and exit nodes", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop, gateway]}
          users={[alice, newcomer]}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: /Machines/u })).toBeVisible();
    await expect.element(screen.getByText("1 connected")).toBeVisible();
    await expect.element(screen.getByText("1 machine, 1 user")).toBeVisible();
    await expect.element(screen.getByText("1 waiting")).toBeVisible();
    await expect.element(screen.getByText("No global exit node")).toBeVisible();
  });

  it("leaves out the tiles the caller may not read", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop]}
          users={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByText("Machines")).toBeVisible();
    await expect.element(screen.getByText("Users")).not.toBeInTheDocument();
  });
});

describe(NeedsAttention, () => {
  it("stays one quiet row without a heading when nothing is waiting", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[laptop]} users={[alice]} me={admin} />),
    );

    await expect.element(screen.getByText("All machines and users are approved")).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Approval settings" })).toBeVisible();
    await expect.element(screen.getByText("Needs attention")).not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("lists what is waiting with the action it needs", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[laptop, gateway]} users={[alice, newcomer]} me={admin} />),
    );

    await expect.element(screen.getByRole("link", { name: "pi-gateway" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Bob" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Approve" }).elements()).toHaveLength(2);
  });

  it("offers no approval to a caller who cannot approve", async () => {
    const screen = await render(app(<NeedsAttention nodes={[gateway]} users={[]} me={reader} />));

    await expect.element(screen.getByText("pi-gateway")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("keeps its heading and its warning while something waits", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[gateway]} users={[alice]} me={admin} />),
    );

    await expect.element(screen.getByText("Needs attention")).toBeVisible();
    await expect
      .element(screen.getByText("Nothing here can reach the tailnet until it is approved"))
      .toBeVisible();
  });
});

describe(GetStarted, () => {
  it("fills the server URL into the command", async () => {
    const screen = await render(
      app(
        <GetStarted
          me={admin}
          onAddMachine={() => {
            // The dialog belongs to the page.
          }}
        />,
      ),
    );

    await expect
      .element(
        screen.getByText(
          `tailscale up --login-server=${globalThis.location.origin} --authkey=<key>`,
          {
            exact: false,
          },
        ),
      )
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Create key" })).toBeEnabled();
  });

  it("disables the key button without the scope", async () => {
    const screen = await render(
      app(
        <GetStarted
          me={reader}
          onAddMachine={() => {
            // The dialog belongs to the page.
          }}
        />,
      ),
    );

    await expect.element(screen.getByRole("button", { name: "Create key" })).toBeDisabled();
  });
});
