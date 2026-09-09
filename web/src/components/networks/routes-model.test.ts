import { describe, expect, it } from "vitest";

import type { Network, Node } from "~/api/queries.ts";
import {
  approvableAdvertisers,
  filterGroups,
  groupRoutes,
  groupStatus,
  isLearnedRoute,
  noRouteFilters,
  routeFilterCounts,
  routeFiltersFromSearch,
  searchFromRouteFilters,
} from "~/components/networks/routes-model.ts";
import type { RouteGroup } from "~/components/networks/routes-model.ts";

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

interface NodeSpec {
  readonly available?: string[];
  readonly approved?: string[];
  readonly served?: string[];
  readonly connector?: boolean;
  readonly online?: boolean;
}

function node(id: string, spec: NodeSpec = {}): Node {
  return {
    appConnector: spec.connector ?? false,
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

function byId(groups: readonly RouteGroup[], id: string): RouteGroup {
  const found = groups.find((group) => group.id === id);

  if (found === undefined) {
    throw new Error(`no group ${id}`);
  }

  return found;
}

describe(isLearnedRoute, () => {
  it("takes the single addresses of an app connector", () => {
    const connector = node("1", { connector: true });

    expect(isLearnedRoute(connector, "10.0.0.5/32")).toBe(true);
    expect(isLearnedRoute(connector, "fd7a::5/128")).toBe(true);
  });

  it("leaves the subnets, the bare addresses and the machines that are no connector", () => {
    const connector = node("1", { connector: true });

    expect(isLearnedRoute(connector, "10.0.0.0/24")).toBe(false);
    expect(isLearnedRoute(connector, "10.0.0.5")).toBe(false);
    expect(isLearnedRoute(connector, "fd7a::5/32")).toBe(false);
    expect(isLearnedRoute(node("2"), "10.0.0.5/32")).toBe(false);
  });
});

describe(groupRoutes, () => {
  it("puts the machines advertising one prefix under it, in name order", () => {
    const nodes = [
      node("2", { available: ["10.0.0.0/24"], approved: ["10.0.0.0/24"] }),
      node("1", { available: ["10.0.0.0/24"], served: ["10.0.0.0/24"], approved: ["10.0.0.0/24"] }),
    ];

    const group = byId(groupRoutes(nodes, []), "10.0.0.0/24");

    expect(group.advertisers.map((advertiser) => advertiser.node.id)).toStrictEqual(["1", "2"]);
    expect(group.advertisers.map((advertiser) => advertiser.primary)).toStrictEqual([true, false]);
    expect(group.pending).toBe(0);
  });

  it("counts what waits and folds the exit pair of every machine into one group", () => {
    const nodes = [
      node("1", { available: ["0.0.0.0/0", "::/0"], approved: ["0.0.0.0/0", "::/0"] }),
      node("2", { available: ["0.0.0.0/0", "::/0", "10.0.0.0/24"] }),
    ];

    const groups = groupRoutes(nodes, []);
    const exit = byId(groups, "exit");

    expect(groups.map((group) => group.id)).toStrictEqual(["exit", "10.0.0.0/24"]);
    expect(exit.advertisers).toHaveLength(2);
    expect(exit.pending).toBe(1);
    // No primary election on the exit routes: every approved exit node serves its own clients.
    expect(exit.advertisers.every((advertiser) => !advertiser.primary)).toBe(true);
  });

  it("marks a prefix learned only while every machine advertising it learned it", () => {
    const learned = node("1", { available: ["10.0.0.5/32"], connector: true });
    const byHand = node("2", { available: ["10.0.0.5/32"] });

    expect(byId(groupRoutes([learned], []), "10.0.0.5/32").learned).toBe(true);
    expect(byId(groupRoutes([learned, byHand], []), "10.0.0.5/32").learned).toBe(false);
  });

  it("collects the networks that own the prefix on any of its machines", () => {
    const nodes = [
      node("1", { available: ["10.0.0.0/24"] }),
      node("2", { available: ["10.0.0.0/24"] }),
    ];
    const networks = [network("7", ["1"], ["10.0.0.0/24"]), network("8", ["2"], ["10.0.0.0/24"])];

    expect(byId(groupRoutes(nodes, networks), "10.0.0.0/24").networks).toHaveLength(2);
  });
});

describe(groupStatus, () => {
  const prefix = "10.0.0.0/24";

  it("stays approved while one machine still advertises the prefix", () => {
    const group = byId(
      groupRoutes(
        [node("1", { available: [prefix], approved: [prefix] }), node("2", { approved: [prefix] })],
        [],
      ),
      prefix,
    );

    expect(group.stale).toBe(1);
    expect(groupStatus(group)).toBe("approved");
  });

  it("waits on the machines that still advertise it, past the ones that stopped", () => {
    const group = byId(
      groupRoutes([node("1", { available: [prefix] }), node("2", { approved: [prefix] })], []),
      prefix,
    );

    expect(group.pending).toBe(1);
    expect(group.stale).toBe(1);
    expect(groupStatus(group)).toBe("pending");
  });

  it("is no longer advertised only once every machine stopped advertising it", () => {
    const group = byId(
      groupRoutes([node("1", { approved: [prefix] }), node("2", { approved: [prefix] })], []),
      prefix,
    );

    expect(group.stale).toBe(2);
    expect(groupStatus(group)).toBe("stale");
  });
});

describe(filterGroups, () => {
  const groups = groupRoutes(
    [
      node("1", { available: ["10.0.0.0/24"] }),
      node("2", { available: ["10.9.0.0/24"], approved: ["10.9.0.0/24"] }),
      node("3", { available: ["10.0.0.5/32"], approved: ["10.0.0.5/32"], connector: true }),
      node("4", { available: ["0.0.0.0/0", "::/0"], approved: ["0.0.0.0/0", "::/0"] }),
    ],
    [network("7", ["2"], ["10.9.0.0/24"])],
  );

  it("keeps everything while no chip is on", () => {
    expect(filterGroups(groups, noRouteFilters)).toHaveLength(4);
  });

  it("widens across the kinds and narrows by pending", () => {
    const kinds = filterGroups(groups, { ...noRouteFilters, exit: true, learned: true });

    expect(kinds.map((group) => group.id)).toStrictEqual(["10.0.0.5/32", "exit"]);
    expect(
      filterGroups(groups, { ...noRouteFilters, pending: true }).map((group) => group.id),
    ).toStrictEqual(["10.0.0.0/24"]);
    expect(filterGroups(groups, { ...noRouteFilters, pending: true, exit: true })).toStrictEqual(
      [],
    );
  });

  it("counts what each chip would keep", () => {
    expect(routeFilterCounts(groups)).toStrictEqual({
      pending: 1,
      exit: 1,
      learned: 1,
      networks: 1,
    });
  });
});

describe(approvableAdvertisers, () => {
  it("skips the machines whose approval belongs to an enabled network", () => {
    const nodes = [
      node("1", { available: ["10.0.0.0/24"] }),
      node("2", { available: ["10.0.0.0/24"] }),
      node("3", { available: ["10.0.0.0/24"] }),
    ];
    const networks = [
      network("7", ["2"], ["10.0.0.0/24"]),
      { ...network("8", ["3"], ["10.0.0.0/24"]), enabled: false },
    ];

    const waiting = approvableAdvertisers(byId(groupRoutes(nodes, networks), "10.0.0.0/24"));

    expect(waiting.map((advertiser) => advertiser.node.id)).toStrictEqual(["1", "3"]);
  });
});

describe(routeFiltersFromSearch, () => {
  it("reads only true as on and writes the on ones back", () => {
    const state = routeFiltersFromSearch({ pending: true, exit: "yes", learned: undefined });

    expect(state).toStrictEqual({ pending: true, exit: false, learned: false, networks: false });
    expect(searchFromRouteFilters(state)).toStrictEqual({ pending: true });
    expect(searchFromRouteFilters(noRouteFilters)).toStrictEqual({});
  });
});
