import { describe, expect, it } from "vitest";

import type { Node } from "~/api/queries.ts";
import { isExitRoute, pendingRouteCount } from "~/lib/node.ts";

function fakeNode(availableRoutes: readonly string[], approvedRoutes: readonly string[]): Node {
  return {
    appConnector: false,
    remoteConfig: false,
    sshServer: false,
    approved: true,
    approvedAt: "2099-01-01T00:00:00Z",
    announcedServices: [],
    approvedRoutes: [...approvedRoutes],
    approvedServices: [],
    availableRoutes: [...availableRoutes],
    clientWarnings: [],
    createdAt: "2099-01-01T00:00:00Z",
    discoKey: "",
    ephemeral: false,
    expiry: "2099-01-01T00:00:00Z",
    givenName: "test-node",
    globalExitNode: false,
    funnelEnabled: false,
    clientVersion: "",
    os: "",
    osVersion: "",
    updateAvailable: false,
    id: "1",
    ipAddresses: [],
    lastSeen: "2099-01-01T00:00:00Z",
    machineKey: "",
    name: "test-node",
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
      user: {
        approved: true,
        approvedAt: "2099-01-01T00:00:00Z",
        createdAt: "2099-01-01T00:00:00Z",
        displayName: "",
        email: "",
        id: "1",
        name: "dev",
        profilePicUrl: "",
        provider: "",
        providerId: "",
        role: "member",
      },
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    subnetRoutes: [],
    suspended: false,
    suspendedAt: null,
    tags: [],
    user: {
      approved: true,
      approvedAt: "2099-01-01T00:00:00Z",
      createdAt: "2099-01-01T00:00:00Z",
      displayName: "",
      email: "",
      id: "1",
      name: "dev",
      profilePicUrl: "",
      provider: "",
      providerId: "",
      role: "member",
    },
  };
}

describe(isExitRoute, () => {
  it("recognizes the default IPv4 and IPv6 routes", () => {
    expect(isExitRoute("0.0.0.0/0")).toBe(true);
    expect(isExitRoute("::/0")).toBe(true);
    expect(isExitRoute("10.0.0.0/24")).toBe(false);
  });
});

describe(pendingRouteCount, () => {
  it("counts zero when no routes are advertised", () => {
    const node = fakeNode([], []);
    expect(pendingRouteCount(node)).toBe(0);
  });

  it("counts each pending subnet separately", () => {
    const node = fakeNode(["10.0.0.0/24", "192.168.1.0/24"], []);
    expect(pendingRouteCount(node)).toBe(2);
  });

  it("folds the pending exit pair into one", () => {
    const node = fakeNode(["0.0.0.0/0", "::/0"], []);
    expect(pendingRouteCount(node)).toBe(1);
  });

  it("folds one pending exit route into one", () => {
    const node = fakeNode(["0.0.0.0/0"], []);
    expect(pendingRouteCount(node)).toBe(1);
  });

  it("counts subnets alongside a folded exit pair", () => {
    const node = fakeNode(["0.0.0.0/0", "::/0", "10.0.0.0/24"], []);
    expect(pendingRouteCount(node)).toBe(2);
  });

  it("ignores approved routes", () => {
    const node = fakeNode(["0.0.0.0/0", "::/0", "10.0.0.0/24"], ["0.0.0.0/0", "::/0"]);
    expect(pendingRouteCount(node)).toBe(1);
  });

  it("returns zero when everything is approved", () => {
    const node = fakeNode(
      ["0.0.0.0/0", "::/0", "10.0.0.0/24"],
      ["0.0.0.0/0", "::/0", "10.0.0.0/24"],
    );
    expect(pendingRouteCount(node)).toBe(0);
  });
});
