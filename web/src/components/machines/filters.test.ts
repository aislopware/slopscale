import { describe, expect, it } from "vitest";

import type { Node, User } from "~/api/queries.ts";
import { filterNodes, statusCounts, toStatusFilter } from "~/components/machines/filters.ts";

const now = new Date("2026-01-01T12:00:00Z");
const stamp = now.toISOString();

const ada: User = {
  approved: true,
  approvedAt: stamp,
  createdAt: stamp,
  displayName: "Ada",
  email: "ada@example.com",
  id: "1",
  name: "ada",
  profilePicUrl: "",
  provider: "",
  providerId: "",
  role: "member",
};

function node(id: string, overrides: Partial<Node> = {}): Node {
  return {
    approved: true,
    approvedAt: stamp,
    approvedRoutes: [],
    availableRoutes: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    ephemeral: false,
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
      user: ada,
    },
    registerMethod: "REGISTER_METHOD_CLI",
    sharedWith: [],
    subnetRoutes: [],
    suspended: false,
    suspendedAt: null,
    tags: [],
    user: ada,
    ...overrides,
  };
}

const connected = node("1");
const offline = node("2", { online: false });
const pending = node("3", { approved: false });
const expired = node("4", { online: false, expiry: "2025-01-01T00:00:00Z" });
const suspended = node("5", { suspended: true, suspendedAt: "2025-06-01T00:00:00Z" });
const everything = [connected, offline, pending, expired, suspended];

describe(filterNodes, () => {
  it("keeps everything for the default tab", () => {
    expect(filterNodes(everything, { status: "all", user: "" }, now)).toHaveLength(5);
  });

  it("counts an expired key and a suspended machine as offline, since neither is reachable", () => {
    const ids = filterNodes(everything, { status: "offline", user: "" }, now).map((row) => row.id);

    expect(ids).toStrictEqual(["2", "4", "5"]);
  });

  it("separates connected from waiting for approval", () => {
    expect(filterNodes(everything, { status: "online", user: "" }, now)).toStrictEqual([connected]);
    expect(filterNodes(everything, { status: "pending", user: "" }, now)).toStrictEqual([pending]);
  });

  it("matches the owner and anyone the machine is shared with", () => {
    const shared = node("6", { user: { ...ada, id: "9" }, sharedWith: ["1"] });
    const rows = filterNodes([connected, shared], { status: "all", user: "1" }, now);

    expect(rows).toStrictEqual([connected, shared]);
  });
});

describe(statusCounts, () => {
  it("counts every tab, with all as the total", () => {
    expect(statusCounts(everything, now)).toStrictEqual({
      all: 5,
      online: 1,
      offline: 3,
      pending: 1,
    });
  });
});

describe(toStatusFilter, () => {
  it("falls back to all for anything the URL does not know", () => {
    const search: { status?: string } = {};

    expect(toStatusFilter("pending")).toBe("pending");
    expect(toStatusFilter("expired")).toBe("all");
    expect(toStatusFilter(search.status)).toBe("all");
  });
});
