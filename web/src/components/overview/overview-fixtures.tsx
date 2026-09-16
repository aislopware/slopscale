/** Shared fixtures for the overview tests, which are split across files by the size limit. */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";

export const admin: Me = {
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

export const reader: Me = { ...admin, role: "auditor", permissions: { "devices:core:read": true } };

export const approver: Me = { ...admin, permissions: { ...admin.permissions, policy_file: true } };

export const ops: Group = {
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

export const waitingRequest: AccessRequest = {
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

export const alice: User = {
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

export const newcomer: User = {
  ...alice,
  approved: false,
  approvedAt: null,
  displayName: "Bob",
  email: "bob@example.com",
  id: "2",
  name: "bob",
  role: "member",
};

export const laptop: Node = {
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

export const gateway: Node = {
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
export function app(children: ReactNode): ReactElement {
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
