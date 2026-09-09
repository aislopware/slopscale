import type { Network, Node } from "~/api/queries.ts";
import { toRouteRows } from "~/components/networks/model.ts";
import type { RouteStatus } from "~/components/networks/model.ts";
import { isIpv4, isIpv6 } from "~/lib/ip.ts";
import { isExitRoute, nodeName } from "~/lib/node.ts";

const singleIpv4Bits = "32";
const singleIpv6Bits = "128";

/**
 * A route an app connector learned: one address, advertised because a domain the app names resolved
 * to it. A tailnet running a connector grows hundreds of them, so they are worth pulling out of the
 * list of subnets an operator chose to advertise.
 */
export function isLearnedRoute(node: Node, route: string): boolean {
  if (!node.appConnector) {
    return false;
  }

  const [address, bits, ...rest] = route.split("/");

  if (address === undefined || rest.length > 0) {
    return false;
  }

  return (
    (bits === singleIpv4Bits && isIpv4(address)) || (bits === singleIpv6Bits && isIpv6(address))
  );
}

/**
 * Whether the machine is the one serving the prefix. The server elects one primary router per
 * prefix and reports it as the machine's `subnetRoutes`; the others stand by. Exit routes have no
 * election, every approved exit node serves its own clients, so they are never marked.
 */
export function isPrimaryRoute(node: Node, route: string): boolean {
  return !isExitRoute(route) && node.subnetRoutes.includes(route);
}

/** One machine advertising one prefix: the child row under a prefix several machines advertise. */
export interface RouteAdvertiser {
  readonly kind: "advertiser";
  readonly id: string;
  readonly node: Node;
  readonly route: string;
  readonly exit: boolean;
  readonly status: RouteStatus;
  /** The machine the tailnet routes the prefix through, of the ones that advertise it. */
  readonly primary: boolean;
  readonly learned: boolean;
  /** The networks that made, or would make, this machine's approval. */
  readonly networks: readonly Network[];
}

/** One prefix, with every machine that advertises it. */
export interface RouteGroup {
  readonly kind: "group";
  readonly id: string;
  readonly route: string;
  readonly exit: boolean;
  readonly advertisers: readonly RouteAdvertiser[];
  /** Every network that owns the prefix on any of its advertisers. */
  readonly networks: readonly Network[];
  /** How many advertisers wait for an approval. */
  readonly pending: number;
  /** How many advertisers keep an approval for a prefix they stopped advertising. */
  readonly stale: number;
  /** Whether every machine advertising it learned it through an app connector. */
  readonly learned: boolean;
}

export type RoutesRow = RouteAdvertiser | RouteGroup;

export function isRouteGroup(row: RoutesRow): row is RouteGroup {
  return row.kind === "group";
}

/**
 * Every advertised prefix once, with the machines advertising it under it, so a prefix two subnet
 * routers both offer reads as one route with a standby rather than as two unrelated rows. The exit
 * routes are one group, as they are one row per machine elsewhere.
 */
export function groupRoutes(nodes: readonly Node[], networks: readonly Network[]): RouteGroup[] {
  const groups = new Map<string, RouteAdvertiser[]>();

  for (const row of toRouteRows(nodes, networks)) {
    const key = row.exit ? "exit" : row.route;
    const advertisers = groups.get(key) ?? [];

    advertisers.push({
      kind: "advertiser",
      id: row.id,
      node: row.node,
      route: row.route,
      exit: row.exit,
      status: row.status,
      primary: isPrimaryRoute(row.node, row.route),
      learned: isLearnedRoute(row.node, row.route),
      networks: row.networks,
    });
    groups.set(key, advertisers);
  }

  return [...groups].map(([id, advertisers]) => buildGroup(id, advertisers));
}

function buildGroup(id: string, advertisers: RouteAdvertiser[]): RouteGroup {
  const sorted = advertisers.toSorted((left, right) =>
    nodeName(left.node).localeCompare(nodeName(right.node)),
  );
  const [first] = sorted;

  return {
    kind: "group",
    id,
    route: first?.route ?? id,
    exit: first?.exit ?? false,
    advertisers: sorted,
    networks: uniqueNetworks(sorted),
    pending: sorted.filter((advertiser) => advertiser.status === "pending").length,
    stale: sorted.filter((advertiser) => advertiser.status === "stale").length,
    learned: sorted.every((advertiser) => advertiser.learned),
  };
}

/**
 * The state a prefix reads as. Only a prefix every one of its machines stopped advertising is no
 * longer advertised; while one machine still offers it the prefix is served, so the state comes
 * from the machines that still advertise it and the stale ones are counted beside them instead.
 */
export function groupStatus(group: RouteGroup): RouteStatus {
  if (group.advertisers.length > 0 && group.stale === group.advertisers.length) {
    return "stale";
  }

  return group.pending > 0 ? "pending" : "approved";
}

function uniqueNetworks(advertisers: readonly RouteAdvertiser[]): Network[] {
  const seen = new Map<string, Network>();

  for (const advertiser of advertisers) {
    for (const network of advertiser.networks) {
      seen.set(network.id, network);
    }
  }

  return [...seen.values()];
}

/**
 * The advertisers a group approval would cover: the ones still waiting, minus any whose approval
 * belongs to an enabled network, which is where the single-row button stops too.
 */
export function approvableAdvertisers(group: RouteGroup): RouteAdvertiser[] {
  return group.advertisers.filter(
    (advertiser) =>
      advertiser.status === "pending" && !advertiser.networks.some((network) => network.enabled),
  );
}

/** The chips above the table. Each one is off by default and absent from the URL while it is. */
export const routeFilters = ["pending", "exit", "learned", "networks"] as const;

export type RouteFilter = (typeof routeFilters)[number];

export const routeFilterLabels: Record<RouteFilter, string> = {
  pending: "Pending",
  exit: "Exit routes",
  learned: "Learned",
  networks: "Networks",
};

export type RouteFilterState = Record<RouteFilter, boolean>;

export const noRouteFilters: RouteFilterState = {
  pending: false,
  exit: false,
  learned: false,
  networks: false,
};

/** What each chip asks of a prefix. */
const filterTests: Record<RouteFilter, (group: RouteGroup) => boolean> = {
  pending: (group) => group.pending > 0,
  exit: (group) => group.exit,
  learned: (group) => group.learned,
  networks: (group) => group.networks.length > 0,
};

function matchesFilter(group: RouteGroup, filter: RouteFilter): boolean {
  return filterTests[filter](group);
}

/**
 * The groups the chips keep. Pending narrows whatever else is on, because it asks about a route's
 * state; the other three name kinds of route and so widen each other, which is what makes "exit
 * routes and the learned ones" a question the chips can ask at all.
 */
export function filterGroups(groups: readonly RouteGroup[], state: RouteFilterState): RouteGroup[] {
  const kinds = routeFilters.filter((filter) => filter !== "pending" && state[filter]);

  return groups.filter((group) => {
    if (state.pending && !matchesFilter(group, "pending")) {
      return false;
    }

    return kinds.length === 0 || kinds.some((filter) => matchesFilter(group, filter));
  });
}

/** How many groups each chip would keep on its own, for the count beside its label. */
export function routeFilterCounts(groups: readonly RouteGroup[]): Record<RouteFilter, number> {
  const counts: Record<RouteFilter, number> = { pending: 0, exit: 0, learned: 0, networks: 0 };

  for (const filter of routeFilters) {
    counts[filter] = groups.filter((group) => matchesFilter(group, filter)).length;
  }

  return counts;
}

/** The chips as a URL carries them: each one is absent while it is off, and may be nonsense. */
export type RawRouteSearch = Partial<Record<RouteFilter, unknown>>;

/** The chips a URL asks for. Anything but true reads as off, so a stray value turns nothing on. */
export function routeFiltersFromSearch(search: RawRouteSearch): RouteFilterState {
  const state = { ...noRouteFilters };

  for (const filter of routeFilters) {
    state[filter] = search[filter] === true;
  }

  return state;
}

/**
 * The URL for a chip state. A chip that is off is absent, so `/routes` and `/routes?q=connector-1`,
 * which is what the apps page links to for a connector's pending routes, stay the URLs this page
 * produces for the same view.
 */
export function searchFromRouteFilters(
  state: RouteFilterState,
): Partial<Record<RouteFilter, true>> {
  const search: Partial<Record<RouteFilter, true>> = {};

  for (const filter of routeFilters) {
    if (state[filter]) {
      search[filter] = true;
    }
  }

  return search;
}
