import { describe, expect, it } from "vitest";

import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import {
  groupsOfNode,
  groupsOfUser,
  machineCount,
  portsError,
  protocolSummary,
  rulesUsingGroup,
} from "~/components/access/model.ts";

const stamp = "2026-01-01T12:00:00Z";

function user(id: string): User {
  return {
    approved: true,
    approvedAt: stamp,
    createdAt: stamp,
    displayName: `User ${id}`,
    email: `${id}@example.com`,
    id,
    name: `user-${id}`,
    profilePicUrl: "",
    provider: "",
    providerId: "",
    role: "member",
  };
}

const ada = user("1");
const bob = user("2");

function node(id: string, owner: User, tags: string[] = []): Node {
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
    tags,
    user: owner,
  };
}

function group(id: string, overrides: Partial<Group> = {}): Group {
  return {
    builtin: "",
    createdAt: stamp,
    description: "",
    expiries: [],
    id,
    name: `group-${id}`,
    nodeIds: [],
    requestable: false,
    updatedAt: stamp,
    userIds: [],
    ...overrides,
  };
}

function rule(id: string, sources: string[], destinations: string[]): AccessRule {
  return {
    bidirectional: false,
    createdAt: stamp,
    description: "",
    destinationGroupIds: destinations,
    enabled: true,
    expiresAt: null,
    id,
    postureIds: [],
    name: `rule-${id}`,
    ports: "",
    protocol: "all",
    sourceGroupIds: sources,
    updatedAt: stamp,
  };
}

const laptop = node("10", ada);
const server = node("11", bob, ["tag:server"]);
const adaSecond = node("12", ada);

const all = group("1", { builtin: "all", name: "All" });
const engineers = group("2", { userIds: [ada.id] });
const servers = group("3", { nodeIds: [server.id] });
const mixed = group("4", { nodeIds: [laptop.id], userIds: [ada.id] });
const groups = [all, engineers, servers, mixed];

describe(groupsOfNode, () => {
  it("includes the builtin group, direct membership and the owner's groups", () => {
    expect(groupsOfNode(groups, laptop).map((item) => item.id)).toStrictEqual(["1", "2", "4"]);
  });

  it("ignores the owner of a tagged machine", () => {
    expect(groupsOfNode(groups, server).map((item) => item.id)).toStrictEqual(["1", "3"]);
  });
});

describe(groupsOfUser, () => {
  it("lists only groups the user is a member of", () => {
    expect(groupsOfUser(groups, ada).map((item) => item.id)).toStrictEqual(["2", "4"]);
    expect(groupsOfUser(groups, bob)).toHaveLength(0);
  });
});

describe(machineCount, () => {
  const nodes = [laptop, server, adaSecond];

  it("counts every machine for the builtin group", () => {
    expect(machineCount(all, nodes)).toBe(3);
  });

  it("counts a machine once when it is in through both paths", () => {
    expect(machineCount(mixed, nodes)).toBe(2);
    expect(machineCount(engineers, nodes)).toBe(2);
    expect(machineCount(servers, nodes)).toBe(1);
  });
});

describe(rulesUsingGroup, () => {
  const rules = [rule("1", ["2"], ["3"]), rule("2", ["1"], ["1"])];

  it("finds the group on either side", () => {
    expect(rulesUsingGroup(rules, engineers).map((item) => item.id)).toStrictEqual(["1"]);
    expect(rulesUsingGroup(rules, servers).map((item) => item.id)).toStrictEqual(["1"]);
    expect(rulesUsingGroup(rules, all).map((item) => item.id)).toStrictEqual(["2"]);
    expect(rulesUsingGroup(rules, mixed)).toHaveLength(0);
  });
});

describe(protocolSummary, () => {
  it("spells out the protocol and its ports", () => {
    expect(protocolSummary({ protocol: "all", ports: "" })).toBe("Any protocol");
    expect(protocolSummary({ protocol: "tcp", ports: "" })).toBe("TCP · any port");
    expect(protocolSummary({ protocol: "udp", ports: "53,5000-5100" })).toBe("UDP 53, 5000-5100");
    expect(protocolSummary({ protocol: "icmp", ports: "" })).toBe("ICMP");
  });
});

describe(portsError, () => {
  it("accepts an empty list, single ports and ranges", () => {
    expect(portsError("")).toBeNull();
    expect(portsError("22")).toBeNull();
    expect(portsError("22, 80-90, 443")).toBeNull();
  });

  it("rejects text, zero, out-of-range and backwards ranges", () => {
    expect(portsError("ssh")).not.toBeNull();
    expect(portsError("0")).not.toBeNull();
    expect(portsError("70000")).not.toBeNull();
    expect(portsError("90-80")).not.toBeNull();
  });
});
