import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";

/** What the policy writes for "every port", and for "any login" in an SSH rule. */
export const everything = "*";

/** The protocols the policy names on their own, because they have no ports. */
const portlessProtocols = new Set(["icmp", "ipv6-icmp"]);

export interface PortSpec {
  /** The protocol the rule named, or null when it named none and the entry is bare ports. */
  readonly protocol: string | null;
  /** The port or range: "*" for every port, "" for a protocol that has none. */
  readonly ports: string;
}

/**
 * One entry of an edge's ports, read the way the policy wrote it. A rule without a protocol is `22`
 * or `1-100`, one with a protocol is `tcp:22` or `udp:1-100`, a protocol with no ports is `icmp`,
 * and `*` on its own is everything.
 */
export function parsePort(entry: string): PortSpec {
  const colon = entry.indexOf(":");

  if (colon === -1) {
    return portlessProtocols.has(entry)
      ? { protocol: entry, ports: "" }
      : { protocol: null, ports: entry };
  }

  return { protocol: entry.slice(0, colon), ports: entry.slice(colon + 1) };
}

/**
 * The chips an edge's ports become: one per protocol, in the order the policy listed them, with
 * that protocol's ports gathered behind it. A protocol whose ports include `*` is open on all of
 * them, so the rest of its list says nothing and is dropped.
 */
export function portChips(ports: readonly string[]): string[] {
  const byProtocol = new Map<string, string[]>();

  for (const entry of ports) {
    const spec = parsePort(entry);
    const ranges = byProtocol.get(spec.protocol ?? "");

    if (ranges === undefined) {
      byProtocol.set(spec.protocol ?? "", spec.ports === "" ? [] : [spec.ports]);
    } else if (spec.ports !== "" && !ranges.includes(spec.ports)) {
      ranges.push(spec.ports);
    }
  }

  return [...byProtocol].map(([protocol, ranges]) => chipText(protocol, ranges));
}

function chipText(protocol: string, ranges: readonly string[]): string {
  const every = ranges.includes(everything);

  // Bare ports under every protocol; "*" alone means everything, which reads as nothing at all.
  if (protocol === "") {
    return every ? "Every port" : ranges.join(", ");
  }

  if (ranges.length === 0) {
    return protocol;
  }

  return `${protocol}:${every ? everything : ranges.join(", ")}`;
}

/** The ports of an edge on one line, for a hover or a screen reader. */
export function summarisePorts(ports: readonly string[]): string {
  const chips = portChips(ports);

  return chips.length === 0 ? "No ports" : chips.join(" · ");
}

/** The logins an edge's SSH rules open, in words; empty when SSH is closed. */
export function sshLogins(users: readonly string[]): string {
  if (users.length === 0) {
    return "";
  }

  return users.includes(everything) ? "Any login" : users.join(", ");
}

/** The same thing mid-sentence, with the fresh sign-in the rule may ask for. */
export function sshPhrase(edge: AccessGraphEdge): string {
  if (edge.sshUsers.length === 0) {
    return "";
  }

  const logins = edge.sshUsers.includes(everything) ? "any login" : edge.sshUsers.join(", ");

  return edge.sshCheck ? `SSH as ${logins}, after a check` : `SSH as ${logins}`;
}

function capabilityCount(count: number): string {
  return count === 1 ? "1 capability" : `${count} capabilities`;
}

/** Everything one edge opens, on one line: what a matrix cell says on hover. */
export function edgeSummary(edge: AccessGraphEdge): string {
  const parts: string[] = [];

  if (edge.ports.length > 0) {
    parts.push(summarisePorts(edge.ports));
  }

  if (edge.routes.length > 0) {
    parts.push(`Routes ${edge.routes.join(", ")}`);
  }

  const ssh = sshPhrase(edge);

  if (ssh !== "") {
    parts.push(ssh);
  }

  if (edge.capabilities.length > 0) {
    parts.push(capabilityCount(edge.capabilities.length));
  }

  return parts.length === 0 ? "Nothing open" : parts.join(" · ");
}

export interface EdgeGroups {
  /** The edges the machine is the source of: what it may reach. */
  readonly reachable: readonly AccessGraphEdge[];
  /** The edges the machine is the destination of: what may reach it. */
  readonly reachedBy: readonly AccessGraphEdge[];
}

/**
 * The edges of one machine, split by direction. The server can narrow the graph to a machine, but
 * splitting here means the same page works on a whole graph it already has.
 */
export function groupEdges(edges: readonly AccessGraphEdge[], nodeId: string): EdgeGroups {
  return {
    reachable: edges.filter((edge) => edge.src === nodeId && edge.dst !== nodeId),
    reachedBy: edges.filter((edge) => edge.dst === nodeId && edge.src !== nodeId),
  };
}

/** Which end of an edge the other machine is. */
export type Peer = "src" | "dst";

export interface EdgeRow {
  readonly edge: AccessGraphEdge;
  /** The machine at the other end, by id, since the graph may not list it. */
  readonly nodeId: string;
  readonly node: AccessGraphNode | undefined;
}

/**
 * One direction's edges as rows, named and in name order. The server orders edges by id, which is
 * the order machines were added, not an order anyone reads a list in.
 */
export function edgeRows(
  edges: readonly AccessGraphEdge[],
  peer: Peer,
  index: ReadonlyMap<string, AccessGraphNode>,
): EdgeRow[] {
  const rows = edges.map((edge) => {
    const nodeId = peer === "src" ? edge.src : edge.dst;

    return { edge, nodeId, node: index.get(nodeId) };
  });

  return rows.toSorted((left, right) =>
    (left.node?.name ?? left.nodeId).localeCompare(right.node?.name ?? right.nodeId),
  );
}

/** How many edges one direction lists before the reader asks for the rest. */
export const edgePreview = 25;

export interface EdgeListView {
  /** The rows on screen. */
  readonly rows: readonly EdgeRow[];
  /** How many rows the list is holding back; 0 once it shows every one of them. */
  readonly hidden: number;
}

/**
 * What one direction's list shows. A machine with hundreds of peers would otherwise run for pages
 * and stretch the panel beside it, so a long list stops at {@link edgePreview} until it is opened.
 */
export function edgeListView(rows: readonly EdgeRow[], expanded: boolean): EdgeListView {
  if (expanded || rows.length <= edgePreview) {
    return { rows, hidden: 0 };
  }

  return { rows: rows.slice(0, edgePreview), hidden: rows.length - edgePreview };
}

export function nodesById(nodes: readonly AccessGraphNode[]): Map<string, AccessGraphNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}

/** Who the machine belongs to: its tags, or its owner's login. Tags and users are exclusive. */
export function ownerLabel(node: AccessGraphNode | undefined): string {
  if (node === undefined) {
    return "Unknown machine";
  }

  if (node.tags.length > 0) {
    return node.tags.join(", ");
  }

  return node.user === "" ? "No owner" : node.user;
}

/**
 * How many machines the matrix draws before it stops being readable. Past this the page asks for
 * one machine instead: 61 columns of two-letter cells say less than one machine's two lists.
 */
export const maxMatrixNodes = 60;

export function fitsMatrix(count: number): boolean {
  return count > 0 && count <= maxMatrixNodes;
}

export interface MatrixCell {
  /** The machine the column stands for. */
  readonly dst: AccessGraphNode;
  /** What the row's machine may do to it, absent when the policy opens nothing. */
  readonly edge: AccessGraphEdge | undefined;
  /** Whether the cell is a machine against itself, which the graph never carries an edge for. */
  readonly self: boolean;
}

export interface MatrixRow {
  /** The machine the row stands for. */
  readonly src: AccessGraphNode;
  readonly cells: readonly MatrixCell[];
}

export interface Matrix {
  readonly columns: readonly AccessGraphNode[];
  readonly rows: readonly MatrixRow[];
  /** How many ordered pairs of machines the policy opens. */
  readonly open: number;
}

function pairKey(src: string, dst: string): string {
  return `${src}>${dst}`;
}

/**
 * The whole tailnet as rows of sources against columns of destinations, both in name order so the
 * two axes read the same way.
 */
export function buildMatrix(
  nodes: readonly AccessGraphNode[],
  edges: readonly AccessGraphEdge[],
): Matrix {
  const columns = nodes.toSorted((left, right) => left.name.localeCompare(right.name));
  const byPair = new Map(edges.map((edge) => [pairKey(edge.src, edge.dst), edge]));

  const rows = columns.map((src) => ({
    src,
    cells: columns.map((dst) => ({
      dst,
      edge: src.id === dst.id ? undefined : byPair.get(pairKey(src.id, dst.id)),
      self: src.id === dst.id,
    })),
  }));

  const open = rows.reduce(
    (sum, row) => sum + row.cells.filter((cell) => cell.edge !== undefined).length,
    0,
  );

  return { columns, rows, open };
}

/** Where the matrix's one tab stop sits: a row and a column of the same machine list. */
export interface MatrixPos {
  readonly row: number;
  readonly col: number;
}

/**
 * Where a key moves the focused cell, or undefined when the key is not one the grid answers. The
 * arrows stop at the edges rather than wrapping, and Home and End go to the ends of the row.
 */
export function nextCell(key: string, at: MatrixPos, size: number): MatrixPos | undefined {
  const last = size - 1;

  switch (key) {
    case "ArrowUp": {
      return { row: Math.max(at.row - 1, 0), col: at.col };
    }
    case "ArrowDown": {
      return { row: Math.min(at.row + 1, last), col: at.col };
    }
    case "ArrowLeft": {
      return { row: at.row, col: Math.max(at.col - 1, 0) };
    }
    case "ArrowRight": {
      return { row: at.row, col: Math.min(at.col + 1, last) };
    }
    case "Home": {
      return { row: at.row, col: 0 };
    }
    case "End": {
      return { row: at.row, col: last };
    }
    default: {
      return undefined;
    }
  }
}

/** The focused cell, held inside a matrix that may have grown shorter since it was picked. */
export function clampCell(at: MatrixPos, size: number): MatrixPos {
  const last = Math.max(size - 1, 0);

  return { row: Math.min(at.row, last), col: Math.min(at.col, last) };
}
