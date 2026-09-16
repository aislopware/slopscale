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

import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { GetStarted } from "~/components/overview/get-started.tsx";
import { MetricTiles, preferredGlobalExitNode } from "~/components/overview/metric-tiles.tsx";
import { NeedsAttention } from "~/components/overview/needs-attention.tsx";

const admin: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scoped: false,
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

const approver: Me = { ...admin, permissions: { ...admin.permissions, policy_file: true } };

const ops: Group = {
  builtin: "",
  createdAt: "2026-01-01T00:00:00Z",
  description: "",
  expiries: [],
  id: "3",
  name: "Ops",
  nodeIds: [],
  requestable: true,
  source: "",
  updatedAt: "2026-01-01T00:00:00Z",
  userIds: [],
};

const waitingRequest: AccessRequest = {
  createdAt: "2026-01-02T00:00:00Z",
  decidedAt: null,
  decidedBy: "",
  durationSeconds: 3600,
  expiresAt: null,
  groupId: "3",
  id: "7",
  note: "",
  reason: "on call",
  revokeNote: "",
  revokedAt: null,
  revokedBy: "",
  status: "pending",
  userId: "2",
};

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
  remoteConfig: false,
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
  exitNodePriority: 0,
  funnelEnabled: false,
  clientVersion: "",
  os: "",
  osVersion: "",
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
          requests={undefined}
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

  it("names the global exit node clients take first", async () => {
    const office: Node = {
      ...gateway,
      id: "10",
      givenName: "office",
      name: "office",
      globalExitNode: true,
      exitNodePriority: 20,
    };
    const dc: Node = {
      ...gateway,
      id: "11",
      givenName: "dc",
      name: "dc",
      globalExitNode: true,
      exitNodePriority: 10,
    };

    expect(preferredGlobalExitNode([laptop, dc, office])?.id).toBe("10");
    expect(preferredGlobalExitNode([laptop])).toBeUndefined();
    expect(
      preferredGlobalExitNode([office, { ...dc, exitNodePriority: 20 }]),
      "a shared top priority names nobody",
    ).toBeUndefined();

    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop, dc, office]}
          users={[alice]}
          requests={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByText("office first of 2 global")).toBeVisible();
  });

  it("leaves out the tiles the caller may not read", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop]}
          users={undefined}
          requests={undefined}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByText("Machines")).toBeVisible();
    await expect.element(screen.getByText("Users")).not.toBeInTheDocument();
  });
});

describe("access requests on the overview", () => {
  it("lists a waiting request beside the machines and users", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          groups={[ops]}
          me={approver}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: "Access to Ops" })).toBeVisible();
    await expect.element(screen.getByText(/Access request · on call/u)).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).toBeVisible();
  });

  it("offers no approval for a request to a caller without the policy scope", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          groups={[ops]}
          me={admin}
        />,
      ),
    );

    await expect.element(screen.getByText("Access to Ops")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("counts the grants in effect and what waits on the tile", async () => {
    const screen = await render(
      app(
        <MetricTiles
          nodes={[laptop]}
          users={[alice]}
          requests={[waitingRequest]}
          nodesLoading={false}
          usersLoading={false}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: /Temporary access/u })).toBeVisible();
    await expect.element(screen.getByText("1 request waiting")).toBeVisible();
  });
});

describe(NeedsAttention, () => {
  it("stays one quiet row without a heading when nothing is waiting", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[laptop]} users={[alice]} requests={[]} groups={[]} me={admin} />),
    );

    await expect.element(screen.getByText("Nothing is waiting for a decision")).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Approval settings" })).toBeVisible();
    await expect.element(screen.getByText("Needs attention")).not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("lists what is waiting with the action it needs", async () => {
    const screen = await render(
      app(
        <NeedsAttention
          nodes={[laptop, gateway]}
          users={[alice, newcomer]}
          requests={[]}
          groups={[]}
          me={admin}
        />,
      ),
    );

    await expect.element(screen.getByRole("link", { name: "pi-gateway" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Bob" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Approve" }).elements()).toHaveLength(2);
  });

  it("offers no approval to a caller who cannot approve", async () => {
    const screen = await render(
      app(<NeedsAttention nodes={[gateway]} users={[]} requests={[]} groups={[]} me={reader} />),
    );

    await expect.element(screen.getByText("pi-gateway")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  });

  it("keeps its heading and its warning while something waits", async () => {
    const screen = await render(
      app(
        <NeedsAttention nodes={[gateway]} users={[alice]} requests={[]} groups={[]} me={admin} />,
      ),
    );

    await expect.element(screen.getByText("Needs attention")).toBeVisible();
    await expect
      .element(
        screen.getByText(
          "Machines and users cannot reach the tailnet, and requesters cannot reach what they asked for, until these are decided",
        ),
      )
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
          `tailscale up --login-server=${globalThis.location.origin} --accept-routes --authkey=<key>`,
          {
            exact: false,
          },
        ),
      )
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Create key" })).toBeEnabled();
  });

  it("offers the sign-in command without the scope", async () => {
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

    await expect
      .element(
        screen.getByText(
          `tailscale up --login-server=${globalThis.location.origin} --accept-routes`,
          {
            exact: true,
          },
        ),
      )
      .toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Create key" }))
      .not.toBeInTheDocument();
  });
});
