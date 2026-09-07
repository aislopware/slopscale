import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { ownerId } from "~/components/machines/owner.ts";
import { isTagged } from "~/lib/node.ts";

/** The server's marker for the group that holds every machine. */
export const builtinAll = "all";

export function isBuiltin(group: Group): boolean {
  return group.builtin !== "";
}

/** The groups a machine is in: directly, through its owner, and always the builtin group. */
export function groupsOfNode(groups: readonly Group[], node: Node): Group[] {
  const owner = isTagged(node) ? null : ownerId(node);

  return groups.filter(
    (group) =>
      isBuiltin(group) ||
      group.nodeIds.includes(node.id) ||
      (owner !== null && group.userIds.includes(owner)),
  );
}

/** The groups a user is in by membership, not counting the builtin group. */
export function groupsOfUser(groups: readonly Group[], user: User): Group[] {
  return groups.filter((group) => group.userIds.includes(user.id));
}

/** How many machines a group resolves to, counting each machine once. */
export function machineCount(group: Group, nodes: readonly Node[]): number {
  if (isBuiltin(group)) {
    return nodes.length;
  }

  const ids = new Set(group.nodeIds);

  for (const node of nodes) {
    const owner = isTagged(node) ? null : ownerId(node);

    if (owner !== null && group.userIds.includes(owner)) {
      ids.add(node.id);
    }
  }

  return ids.size;
}

/** The rules that name the group on either side. */
export function rulesUsingGroup(rules: readonly AccessRule[], group: Group): AccessRule[] {
  return rules.filter(
    (rule) => rule.sourceGroupIds.includes(group.id) || rule.destinationGroupIds.includes(group.id),
  );
}

export function groupName(groups: readonly { id: string; name: string }[], id: string): string {
  return groups.find((group) => group.id === id)?.name ?? `Group ${id}`;
}

export const protocols = ["all", "tcp", "udp", "icmp"] as const;
export type Protocol = (typeof protocols)[number];

const protocolLabels: Record<Protocol, string> = {
  all: "Any protocol",
  tcp: "TCP",
  udp: "UDP",
  icmp: "ICMP",
};

export function toProtocol(value: string): Protocol {
  return protocols.find((known) => known === value) ?? "all";
}

export function protocolLabel(protocol: string): string {
  return protocolLabels[toProtocol(protocol)];
}

/** "TCP 22, 443" or "Any protocol"; ports only exist for tcp and udp. */
export function protocolSummary(rule: Pick<AccessRule, "protocol" | "ports">): string {
  const label = protocolLabel(rule.protocol);
  const hasPorts = rule.protocol === "tcp" || rule.protocol === "udp";

  if (!hasPorts) {
    return label;
  }

  return rule.ports === "" ? `${label} · any port` : `${label} ${rule.ports.replaceAll(",", ", ")}`;
}

const portPattern = /^\d{1,5}(?:-\d{1,5})?$/u;
const maxPort = 65_535;

/** A client-side check that matches the server's: a comma list of ports or ranges within 1..65535. */
export function portsError(ports: string): string | null {
  const text = ports.trim();

  if (text === "") {
    return null;
  }

  for (const part of text.split(",")) {
    const item = part.trim();

    if (!portPattern.test(item)) {
      return "Use ports or ranges like 22, 80, 8000-8100.";
    }

    const [first, last = first] = item.split("-").map(Number);

    if (first === undefined || first < 1 || last === undefined || last > maxPort || last < first) {
      return "Ports go from 1 to 65535 and a range must go up.";
    }
  }

  return null;
}
