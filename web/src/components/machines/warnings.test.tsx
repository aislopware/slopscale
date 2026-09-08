import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Node, User } from "~/api/queries.ts";
import { ClientWarnings } from "~/components/machines/warnings.tsx";

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
  providerId: "",
  role: "member",
};

const router: Node = {
  approved: true,
  approvedAt: "2026-01-01T00:00:00Z",
  approvedRoutes: ["10.0.0.0/24"],
  availableRoutes: ["10.0.0.0/24"],
  createdAt: "2026-01-01T00:00:00Z",
  discoKey: "",
  expiry: null,
  givenName: "pi-gateway",
  globalExitNode: false,
  ephemeral: false,
  clientWarnings: ["ip-forwarding-off", "something-new"],
  id: "2",
  ipAddresses: ["100.64.0.2"],
  lastSeen: new Date().toISOString(),
  machineKey: "",
  name: "pi-gateway",
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
  registerMethod: "REGISTER_METHOD_AUTH_KEY",
  sharedWith: [],
  suspended: false,
  suspendedAt: null,
  subnetRoutes: ["10.0.0.0/24"],
  tags: [],
  user: alice,
};

describe(ClientWarnings, () => {
  it("explains a known warning and names an unknown one", async () => {
    const screen = await render(<ClientWarnings node={router} />);

    await expect.element(screen.getByText("IP forwarding is off")).toBeVisible();
    await expect.element(screen.getByText(/net\.ipv4\.ip_forward/u)).toBeVisible();
    await expect.element(screen.getByText("The client reports something-new")).toBeVisible();
  });

  it("renders nothing when the client reports nothing", async () => {
    const screen = await render(<ClientWarnings node={{ ...router, clientWarnings: [] }} />);

    expect(screen.container.textContent).toBe("");
  });
});
