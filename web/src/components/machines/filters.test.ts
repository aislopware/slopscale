import { describe, expect, it } from "vitest";

import type { Node, User } from "~/api/queries.ts";
import {
  anyAttestation,
  filterNodes,
  filtersFromSearch,
  searchFromFilters,
  statusCounts,
  tagOptions,
  toAttestationFilter,
  toStatusFilter,
} from "~/components/machines/filters.ts";

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
    appConnector: false,
    remoteConfig: false,
    sshServer: false,
    approved: true,
    approvedAt: stamp,
    announcedServices: [],
    approvedRoutes: [],
    approvedServices: [],
    availableRoutes: [],
    clientWarnings: [],
    createdAt: stamp,
    discoKey: "discokey:1",
    expiry: null,
    givenName: `machine-${id}`,
    globalExitNode: false,
    funnelEnabled: false,
    clientVersion: "",
    updateAvailable: false,
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
    expect(
      filterNodes(everything, { status: "all", user: "", tag: "", attestation: "any" }, now),
    ).toHaveLength(5);
  });

  it("counts an expired key and a suspended machine as offline, since neither is reachable", () => {
    const ids = filterNodes(
      everything,
      { status: "offline", user: "", tag: "", attestation: "any" },
      now,
    ).map((row) => row.id);

    expect(ids).toStrictEqual(["2", "4", "5"]);
  });

  it("separates connected from waiting for approval", () => {
    expect(
      filterNodes(everything, { status: "online", user: "", tag: "", attestation: "any" }, now),
    ).toStrictEqual([connected]);
    expect(
      filterNodes(everything, { status: "pending", user: "", tag: "", attestation: "any" }, now),
    ).toStrictEqual([pending]);
  });

  it("matches the owner and anyone the machine is shared with", () => {
    const shared = node("6", { user: { ...ada, id: "9" }, sharedWith: ["1"] });
    const rows = filterNodes(
      [connected, shared],
      { status: "all", user: "1", tag: "", attestation: "any" },
      now,
    );

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

describe("the tag filter", () => {
  const tagged = node("7", { tags: ["tag:prod", "tag:web"] });
  const other = node("8", { tags: ["tag:lab"] });

  it("keeps the machines carrying the tag", () => {
    const rows = filterNodes(
      [connected, tagged, other],
      { status: "all", user: "", tag: "tag:prod", attestation: "any" },
      now,
    );

    expect(rows).toStrictEqual([tagged]);
  });

  it("offers every tag in use once, in order", () => {
    expect(tagOptions([connected, tagged, other])).toStrictEqual([
      "tag:lab",
      "tag:prod",
      "tag:web",
    ]);
  });
});

describe("the attestation filter", () => {
  const proven = node("9", {
    hardwareAttestation: {
      attested: true,
      attestedAt: stamp,
      key: "hwattestpub:ab",
      keyChangedAt: null,
    },
  });
  const lost = node("10", {
    hardwareAttestation: {
      attested: false,
      attestedAt: stamp,
      key: "hwattestpub:cd",
      keyChangedAt: stamp,
    },
  });
  const rows = [connected, proven, lost];
  const base = { status: "all", user: "", tag: "" } as const;

  it("keeps only the machines whose key proves them right now", () => {
    expect(filterNodes(rows, { ...base, attestation: "attested" }, now)).toStrictEqual([proven]);
  });

  it("counts a machine that lost attestation with the ones that never had it", () => {
    const ids = filterNodes(rows, { ...base, attestation: "unattested" }, now).map((row) => row.id);

    expect(ids).toStrictEqual(["1", "10"]);
  });

  it("offers the filter only once some machine reports a key", () => {
    expect(anyAttestation([connected])).toBe(false);
    expect(anyAttestation(rows)).toBe(true);
  });

  it("falls back to any for anything the URL does not know", () => {
    const search: { attested?: string } = {};

    expect(toAttestationFilter("attested")).toBe("attested");
    expect(toAttestationFilter("maybe")).toBe("any");
    expect(toAttestationFilter(search.attested)).toBe("any");
  });
});

describe("the filters in the URL", () => {
  it("reads an absent or unknown parameter as that filter's default", () => {
    expect(filtersFromSearch({})).toStrictEqual({
      query: "",
      status: "all",
      user: "",
      tag: "",
      attestation: "any",
    });
    expect(filtersFromSearch({ status: "sideways" }).status).toBe("all");
  });

  it("leaves a filter at its default out of the URL", () => {
    expect(
      searchFromFilters({ query: "", status: "all", user: "", tag: "", attestation: "any" }),
    ).toStrictEqual({});
  });

  it("round trips every filter", () => {
    const search = {
      q: "laptop",
      status: "offline",
      user: "3",
      tag: "tag:prod",
      attested: "attested",
    } as const;

    expect(searchFromFilters(filtersFromSearch(search))).toStrictEqual(search);
  });
});
