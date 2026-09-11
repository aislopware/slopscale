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
 * The lines an edge's ports become: one per protocol, in the order the policy listed them, with
 * that protocol's ports gathered behind it. A protocol whose ports include `*` is open on all of
 * them, so the rest of its list says nothing and is dropped.
 */
export function portLines(ports: readonly string[]): string[] {
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

  return [...byProtocol].map(([protocol, ranges]) => lineText(protocol, ranges));
}

function lineText(protocol: string, ranges: readonly string[]): string {
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
  const lines = portLines(ports);

  return lines.length === 0 ? "No ports" : lines.join(" · ");
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
 * How many access classes the map draws before it stops being readable. Machines with the same
 * access share a row, so a tailnet of hundreds is usually a map of a dozen; a policy written per
 * machine is the one that grows past this, and for that the page asks for one machine instead.
 */
export const maxMapClasses = 60;

export function fitsMap(count: number): boolean {
  return count > 0 && count <= maxMapClasses;
}

/**
 * How many classes fit across a screen as tiles with words in them. Past that a cell shrinks to a
 * square and the words move to the line under the map, so sixty classes still fit on one screen.
 */
export const maxTileClasses = 8;

/** Whether a map's cells are tiles with words in them or bare squares. */
export type MapDensity = "tiles" | "squares";

export function mapDensity(size: number): MapDensity {
  return size <= maxTileClasses ? "tiles" : "squares";
}

/** What one cell says when read out in full: the pair's summary, or why there is none. */
export function cellSummary(cell: MapCell): string {
  if (cell.self) {
    return "One machine against itself";
  }

  return cell.edge === undefined ? "Nothing open" : edgeSummary(cell.edge);
}

/** What an edge opens, as the cell that stands for it is tinted. */
export type Openness = "all" | "some" | "other";

/**
 * How open an edge is: every port, some ports, or nothing on the packet filter but SSH, a route or
 * a capability. Three steps of one tint, so a map reads at a glance which pairs are wide open.
 */
export function openness(edge: AccessGraphEdge): Openness {
  // A bare `*` only: `udp:*` is every port of one protocol, which is still some of them.
  if (edge.ports.some((entry) => entry === everything)) {
    return "all";
  }

  return edge.ports.length > 0 ? "some" : "other";
}

/** The two or three words a cell has room for; the popover carries the rest. */
export function cellLabel(edge: AccessGraphEdge): string {
  const open = openness(edge);
  const ssh = edge.sshUsers.length > 0;

  if (open === "all") {
    return ssh ? "All ports · SSH" : "All ports";
  }

  if (open === "some") {
    const lines = portLines(edge.ports);

    return ssh ? `${lines.join(", ")} · SSH` : lines.join(", ");
  }

  const kinds = [
    ...(ssh ? ["SSH"] : []),
    ...(edge.routes.length > 0 ? ["Routes"] : []),
    ...(edge.capabilities.length > 0 ? ["Capabilities"] : []),
  ];

  // "only" when there is one kind; two or three kinds are listed, so nothing reads as narrower
  // than it is.
  return kinds.length === 1 ? `${kinds[0]} only` : kinds.join(" · ");
}

function edgeLabel(edge: AccessGraphEdge): string {
  return JSON.stringify([
    edge.ports.toSorted(),
    edge.sshUsers.toSorted(),
    edge.sshCheck,
    edge.routes.toSorted(),
    edge.capabilities.toSorted(),
  ]);
}

function pairKey(src: string, dst: string): string {
  return `${src}>${dst}`;
}

/** What every machine opens on every other, as a string per ordered pair, "" for nothing. */
interface Relations {
  readonly nodes: readonly AccessGraphNode[];
  readonly between: (src: string, dst: string) => string;
}

/**
 * Whether two machines may share a class: every third machine sees them the same way in both
 * directions, and what they open on each other is the same both ways. Checking against one member
 * is enough, because the relation is transitive: if a third machine cannot tell one from the other,
 * or the other from a third, it cannot tell the first from the third either.
 */
function alike(one: AccessGraphNode, other: AccessGraphNode, relations: Relations): boolean {
  const { between } = relations;

  return (
    between(one.id, other.id) === between(other.id, one.id) &&
    relations.nodes.every(
      (third) =>
        third.id === one.id ||
        third.id === other.id ||
        (between(one.id, third.id) === between(other.id, third.id) &&
          between(third.id, one.id) === between(third.id, other.id)),
    )
  );
}

/**
 * Machines that the policy treats the same: two machines share a class when every other machine
 * reaches, and is reached by, both of them the same way, and they reach each other the same way.
 * That is the coarsest grouping in which every pair between two classes carries the same edge, so
 * one cell can stand for all of them. Grouping by the classes of a machine's neighbours instead
 * (colour refinement) is coarser but wrong for a map: two pairs that reach only their own partner
 * look alike, and one cell would then say every member reaches every other.
 *
 * A self edge says nothing about how a machine treats others and is left out. Machines are bucketed
 * first by the multiset of what they open on and receive from every other machine, which any two
 * members of a class share, so the pairwise check runs only inside a bucket.
 */
export function accessClasses(
  nodes: readonly AccessGraphNode[],
  edges: readonly AccessGraphEdge[],
): AccessGraphNode[][] {
  const byPair = new Map(
    edges
      .filter((one) => one.src !== one.dst)
      .map((one) => [pairKey(one.src, one.dst), edgeLabel(one)]),
  );
  const relations: Relations = {
    nodes,
    between: (src, dst) => byPair.get(pairKey(src, dst)) ?? "",
  };
  const buckets = new Map<string, AccessGraphNode[][]>();

  for (const node of nodes) {
    const key = nodes
      .filter((other) => other.id !== node.id)
      .map(
        (other) =>
          `${relations.between(node.id, other.id)}\t${relations.between(other.id, node.id)}`,
      )
      .toSorted()
      .join("\n");
    const classes = buckets.get(key) ?? [];
    const home = classes.find(([first]) => first !== undefined && alike(first, node, relations));

    if (home === undefined) {
      classes.push([node]);
    } else {
      home.push(node);
    }
    buckets.set(key, classes);
  }

  return [...buckets.values()].flat();
}

export interface AccessClass {
  readonly id: string;
  /** The machines, in name order. */
  readonly members: readonly AccessGraphNode[];
  /** The machine's name for a class of one; else what the members share: tags, an owner, or owners. */
  readonly label: string;
  /**
   * Under the label: the owner for a class of one, else the first machine and how many more, so two
   * classes with the same owner still read apart.
   */
  readonly detail: string;
  /** Whether the label is tags, which read in the mono face. */
  readonly tagged: boolean;
}

/** How many owners a mixed class names before it counts the rest. */
const namedOwners = 2;

/** A login without its domain: the part that tells people apart, in the room a header has. */
export function shortOwner(node: AccessGraphNode): string {
  if (node.tags.length > 0) {
    return node.tags.join(", ");
  }

  if (node.user === "") {
    return "No owner";
  }

  const at = node.user.indexOf("@");

  return at === -1 ? node.user : node.user.slice(0, at);
}

function classLabel(
  members: readonly AccessGraphNode[],
): Pick<AccessClass, "label" | "detail" | "tagged"> {
  const [only] = members;

  if (only !== undefined && members.length === 1) {
    return { label: only.name, detail: shortOwner(only), tagged: false };
  }

  const detail = `${only?.name ?? ""} +${members.length - 1}`;
  const tags = new Set(members.map((node) => node.tags.join(", ")));
  const [firstTags] = tags;

  if (tags.size === 1 && firstTags !== undefined && firstTags !== "") {
    return { label: firstTags, detail, tagged: true };
  }

  const owners = [...new Set(members.map((node) => shortOwner(node)))].toSorted();

  if (owners.length <= namedOwners) {
    return { label: owners.join(", "), detail, tagged: false };
  }

  const rest = owners.length - namedOwners;

  return { label: `${owners.slice(0, namedOwners).join(", ")} +${rest}`, detail, tagged: false };
}

/** The class a machine is in, or undefined for a machine the map does not list. */
export type ClassIndex = ReadonlyMap<string, AccessClass>;

export interface MapCell {
  readonly dst: AccessClass;
  /** What any member of the row reaches any member of the column with, or nothing at all. */
  readonly edge: AccessGraphEdge | undefined;
  /** A class of one machine against itself, which has no pair to speak of. */
  readonly self: boolean;
}

export interface MapRow {
  readonly src: AccessClass;
  readonly cells: readonly MapCell[];
}

export interface AccessMap {
  readonly classes: readonly AccessClass[];
  readonly rows: readonly MapRow[];
  /** How many ordered pairs of machines the policy opens. */
  readonly open: number;
  /** How many machines the map stands for. */
  readonly machines: number;
}

function byLabel(left: AccessClass, right: AccessClass): number {
  // Owners' machines first, then tagged ones, each run in label order, so the map reads people
  // then servers the way the rules are written.
  if (left.tagged !== right.tagged) {
    return left.tagged ? 1 : -1;
  }

  return left.label.localeCompare(right.label) || right.members.length - left.members.length;
}

/**
 * The tailnet as rows of source classes against columns of destination classes. Any pair between
 * two classes carries the same edge, so one is enough for the cell; a class against itself takes
 * the edge between two of its members.
 */
export function buildAccessMap(
  nodes: readonly AccessGraphNode[],
  edges: readonly AccessGraphEdge[],
): AccessMap {
  const byPair = new Map(edges.map((edge) => [pairKey(edge.src, edge.dst), edge]));
  const classes = accessClasses(nodes, edges)
    .map((members, index): AccessClass => {
      const sorted = members.toSorted((left, right) => left.name.localeCompare(right.name));
      const { label, detail, tagged } = classLabel(sorted);

      return { id: String(index), members: sorted, label, detail, tagged };
    })
    .toSorted(byLabel);

  const edgeBetween = (src: AccessClass, dst: AccessClass): AccessGraphEdge | undefined => {
    const [from] = src.members;
    const to = dst.members.find((node) => node.id !== from?.id);

    return from === undefined || to === undefined ? undefined : byPair.get(pairKey(from.id, to.id));
  };

  const rows = classes.map((src) => ({
    src,
    cells: classes.map((dst) => {
      const self = src.id === dst.id && src.members.length === 1;

      return { dst, edge: self ? undefined : edgeBetween(src, dst), self };
    }),
  }));

  const open = edges.filter((edge) => edge.src !== edge.dst).length;

  return { classes, rows, open, machines: nodes.length };
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
