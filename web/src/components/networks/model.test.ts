import { describe, expect, it } from "vitest";

import type { Network, Node } from "~/api/queries.ts";
import {
  prefixError,
  prefixesSummary,
  toRouteRows,
  withRouteApproved,
} from "~/components/networks/model.ts";

const stamp = "2026-01-01T12:00:00Z";

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

function node(id: string, available: string[], approved: string[]): Node {
  return {
    approved: true,
    approvedAt: stamp,
    approvedRoutes: approved,
    availableRoutes: available,
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    id,
    ipAddresses: [],
    lastSeen: stamp,
    machineKey: "mkey:1",
    name: `host-${id}`,
    nodeKey: "nodekey:1",
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
      user: owner,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    suspended: false,
    suspendedAt: null,
    subnetRoutes: [],
    tags: [],
    user: owner,
  };
}

function network(id: string, routers: string[], prefixes: string[]): Network {
  return {
    createdAt: stamp,
    description: "",
    enabled: true,
    exitNode: false,
    groupIds: ["1"],
    id,
    name: `net-${id}`,
    ports: "",
    prefixes,
    protocol: "all",
    routerNodeIds: routers,
    routers: [],
    updatedAt: stamp,
  };
}

describe(prefixError, () => {
  it.each(["10.0.0.0/24", "10.0.0.5", "0.0.0.0/0", "fd7a::/64", "::/0"])("accepts %s", (value) => {
    expect(prefixError(value)).toBeNull();
  });

  it.each(["office", "10.0.0.0/33", "10.0.0.0/24/8", "fd7a::/129", "/24"])(
    "rejects %s",
    (value) => {
      expect(prefixError(value)).not.toBeNull();
    },
  );
});

describe(prefixesSummary, () => {
  it("folds the exit routes into one word", () => {
    expect(prefixesSummary(["0.0.0.0/0", "::/0", "10.0.0.0/24"])).toBe("Exit node, 10.0.0.0/24");
  });
});

describe(toRouteRows, () => {
  it("folds the exit pair and marks the networks that route each prefix", () => {
    const nodes = [
      node("1", ["10.0.0.0/24", "0.0.0.0/0", "::/0"], ["10.0.0.0/24"]),
      node("2", [], ["10.9.0.0/24"]),
    ];
    const networks = [network("7", ["1"], ["10.0.0.0/24"])];

    const rows = toRouteRows(nodes, networks);

    expect(rows.map((row) => [row.id, row.status, row.networks.length])).toStrictEqual([
      ["1:10.0.0.0/24", "approved", 1],
      ["1:exit", "pending", 0],
      ["2:10.9.0.0/24", "stale", 0],
    ]);
  });
});

describe(withRouteApproved, () => {
  it("adds or removes the exit pair together", () => {
    const advertising = node("1", ["10.0.0.0/24", "0.0.0.0/0", "::/0"], ["10.0.0.0/24"]);

    expect(withRouteApproved(advertising, "0.0.0.0/0", true)).toStrictEqual([
      "10.0.0.0/24",
      "0.0.0.0/0",
      "::/0",
    ]);
    expect(withRouteApproved(node("1", [], ["0.0.0.0/0", "::/0"]), "::/0", false)).toStrictEqual(
      [],
    );
  });
});
