import type { Network, Node } from "~/api/queries.ts";
import { isIpv4, isIpv6 } from "~/lib/ip.ts";
import { exitRoutes, isExitRoute, nodeName } from "~/lib/node.ts";

const maxIpv4Bits = 32;
const maxIpv6Bits = 128;

function maxBitsOf(address: string): number | null {
  if (isIpv4(address)) {
    return maxIpv4Bits;
  }

  return isIpv6(address) ? maxIpv6Bits : null;
}

/** Why the text is not an address or CIDR, or null when it is. The server masks and dedupes. */
export function prefixError(value: string): string | null {
  const [address, bits, ...rest] = value.split("/");

  if (address === undefined || address === "" || rest.length > 0) {
    return `"${value}" is not an address or CIDR.`;
  }

  const maxBits = maxBitsOf(address);

  if (maxBits === null) {
    return `"${value}" is not an address or CIDR.`;
  }

  if (bits === undefined) {
    return null;
  }

  const length = Number(bits);
  const whole = /^\d{1,3}$/v.test(bits) && Number.isInteger(length);

  return whole && length <= maxBits ? null : `"${value}" has a prefix length past /${maxBits}.`;
}

/** The first problem among the prefixes, or null when every one parses. */
export function prefixesError(values: readonly string[]): string | null {
  for (const value of values) {
    const issue = prefixError(value);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

export function isExitNetwork(prefixes: readonly string[]): boolean {
  return prefixes.some((prefix) => isExitRoute(prefix));
}

/** What the network hands out: the exit routes read as one thing. */
export function prefixesSummary(prefixes: readonly string[]): string {
  const subnets = prefixes.filter((prefix) => !isExitRoute(prefix));
  const parts = isExitNetwork(prefixes) ? ["Exit node", ...subnets] : subnets;

  return parts.join(", ");
}

/** The networks that route the prefix through the node; approval belongs to them. */
export function networksRouting(
  networks: readonly Network[],
  nodeId: string,
  route: string,
): Network[] {
  return networks.filter(
    (network) => network.routerNodeIds.includes(nodeId) && network.prefixes.includes(route),
  );
}

/** The networks that name the node as a router. */
export function networksOfNode(networks: readonly Network[], nodeId: string): Network[] {
  return networks.filter((network) => network.routerNodeIds.includes(nodeId));
}

export type RouteStatus = "approved" | "pending" | "stale";

/** One advertised or approved route on one machine, the unit of the routes page. */
export interface RouteRow {
  readonly id: string;
  readonly node: Node;
  readonly route: string;
  readonly status: RouteStatus;
  readonly exit: boolean;
  /** The networks that made or would make the approval. */
  readonly networks: readonly Network[];
  /** For the global search: machine, route and network names in one string. */
  readonly text: string;
}

function routeStatus(node: Node, route: string): RouteStatus {
  if (!node.approvedRoutes.includes(route)) {
    return "pending";
  }

  return node.availableRoutes.includes(route) ? "approved" : "stale";
}

/** Every route of every machine, the exit routes of a machine folded into one row. */
export function toRouteRows(nodes: readonly Node[], networks: readonly Network[]): RouteRow[] {
  const rows: RouteRow[] = [];

  for (const node of nodes) {
    const routes = new Set([...node.availableRoutes, ...node.approvedRoutes]);
    const exit = exitRoutes.some((route) => routes.has(route));

    for (const route of routes) {
      if (!isExitRoute(route)) {
        rows.push(routeRow(networks, node, route));
      }
    }

    if (exit) {
      rows.push(routeRow(networks, node, exitRoutes[0] ?? ""));
    }
  }

  return rows;
}

function routeRow(networks: readonly Network[], node: Node, route: string): RouteRow {
  const using = networksRouting(networks, node.id, route);
  const exit = isExitRoute(route);

  return {
    id: `${node.id}:${exit ? "exit" : route}`,
    node,
    route,
    status: routeStatus(node, route),
    exit,
    networks: using,
    text: [
      nodeName(node),
      exit ? "exit node" : route,
      ...using.map((network) => network.name),
    ].join(" "),
  };
}

/** The node's approved routes with one route, or the exit pair, added or removed. */
export function withRouteApproved(node: Node, route: string, approved: boolean): string[] {
  const routes = isExitRoute(route) ? exitRoutes : [route];
  const rest = node.approvedRoutes.filter((current) => !routes.includes(current));

  return approved ? [...rest, ...routes] : rest;
}
