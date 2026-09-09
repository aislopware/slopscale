import type { Node, Service } from "~/api/queries.ts";
import type { Tone } from "~/components/ui/status.tsx";
import { dnsLabelIssue } from "~/lib/dns-label.ts";

/** Every service name carries it; the label after it is what the operator names and MagicDNS uses. */
export const servicePrefix = "svc:";

export function serviceLabel(name: string): string {
  return name.startsWith(servicePrefix) ? name.slice(servicePrefix.length) : name;
}

/** The full name for a label the operator typed. The server accepts either; the console sends this. */
export function serviceName(label: string): string {
  return servicePrefix + serviceLabel(label.trim().toLowerCase());
}

/** What clients show for the service: its display name, or the label when it has none. */
export function serviceTitle(service: Service): string {
  return service.displayName === "" ? serviceLabel(service.name) : service.displayName;
}

/**
 * Why the label cannot name a service, in the words the rename dialog uses, or null when it can.
 * The label becomes a MagicDNS label under the base domain, so it is a DNS label; the server lower
 * cases it, and saying so beats silently renaming what was typed.
 */
export function serviceNameIssue(label: string): string | null {
  if (label !== label.toLowerCase()) {
    return "must be lower case";
  }

  return dnsLabelIssue(label);
}

const maxPort = 65_535;
const maxPortDigits = 5;

/** The protocols a port specification may name, as tailcfg parses them. */
const protocols = new Set(["tcp", "udp", "sctp", "icmp", "*"]);

const portPattern = new RegExp(`^\\d{1,${maxPortDigits}}$`, "v");

function isPort(text: string): boolean {
  return portPattern.test(text) && Number(text) <= maxPort;
}

/**
 * Why the text is not a protocol and port such as `tcp:443`, `udp:53-60` or `tcp:*`, or null when
 * it is. The server is the authority; this only spares a round trip.
 */
export function portError(value: string): string | null {
  const colon = value.indexOf(":");
  const protocol = value.slice(0, colon).toLowerCase();

  if (colon === -1 || !protocols.has(protocol)) {
    return `"${value}" is not a protocol and port, such as tcp:443.`;
  }

  const range = value.slice(colon + 1);

  if (range === "*") {
    return null;
  }

  const parts = range.split("-");
  const low = parts[0] ?? "";
  const high = parts[1] ?? low;

  if (parts.length > 2 || !isPort(low) || !isPort(high)) {
    return `"${value}" is not a port or a port range, such as 443 or 53-60.`;
  }

  return Number(low) > Number(high) ? `"${value}" ends before it starts.` : null;
}

/** The first problem among the port specifications, or null when every one parses. */
export function portsError(values: readonly string[]): string | null {
  for (const value of values) {
    const issue = portError(value);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

/**
 * How far a service has got towards being reachable. A client reaches it only through a host that
 * announces it, advertises it and has been approved, so each missing step is its own state: the
 * operator fixes an unapproved service here and an unadvertised one on the machine.
 */
export type ServiceReach = "reachable" | "unapproved" | "inactive" | "unhosted";

export function serviceReach(service: Service): ServiceReach {
  if (service.hosts.some((host) => host.announced && host.active && host.approved)) {
    return "reachable";
  }

  if (service.hosts.some((host) => host.announced && host.active)) {
    return "unapproved";
  }

  return service.hosts.length === 0 ? "unhosted" : "inactive";
}

/** How each state reads, coloured the way every other state in the console is. */
export const reachStates: Readonly<Record<ServiceReach, { tone: Tone; label: string }>> = {
  reachable: { tone: "success", label: "Reachable" },
  unapproved: { tone: "warning", label: "Waiting for approval" },
  inactive: { tone: "warning", label: "Not advertised" },
  unhosted: { tone: "neutral", label: "No host" },
};

/** A service with what the table sorts, filters and shows spelled out. */
export interface ServiceRow extends Service {
  /** The name without its svc: prefix, which is the MagicDNS label. */
  readonly label: string;
  readonly reach: ServiceReach;
  /** The host clients are routed to right now; empty while none serves it. */
  readonly primaryHost: string;
  /** Every host by name, so the search matches a machine. */
  readonly hostNames: string;
}

export function toServiceRows(services: readonly Service[]): ServiceRow[] {
  return services.map((service) => ({
    ...service,
    label: serviceLabel(service.name),
    reach: serviceReach(service),
    primaryHost: service.hosts.find((host) => host.primary)?.name ?? "",
    hostNames: service.hosts.map((host) => host.name).join(", "),
  }));
}

/** How many services a client can reach right now; the page header's count. */
export function reachableCount(services: readonly Service[]): number {
  return services.filter((service) => serviceReach(service) === "reachable").length;
}

/**
 * The machine's approved services with one added or removed. The approval endpoint replaces the
 * whole list, so a switch sends the list it wants rather than the one service it changed.
 */
export function withServiceApproved(
  approved: readonly string[],
  name: string,
  on: boolean,
): string[] {
  const rest = approved.filter((current) => current !== name);

  return on ? [...rest, name] : rest;
}

/**
 * The services the machine may host, read from the services rather than the machine: a service
 * lists every node that announces or is approved for it, so the approval switch on a service can
 * send the whole list back without loading the machines.
 */
export function approvedServicesOf(services: readonly Service[], nodeId: string): string[] {
  return services
    .filter((service) => service.hosts.some((host) => host.nodeId === nodeId && host.approved))
    .map((service) => service.name);
}

/** One service on one machine, the unit of the machine page's services section. */
export interface MachineService {
  readonly name: string;
  /** What the machine serves it on, falling back to what the service tells clients. */
  readonly ports: readonly string[];
  /** Whether the machine reports it in its serve configuration. */
  readonly announced: boolean;
  /** Whether the machine advertises it, so it may receive traffic. */
  readonly active: boolean;
  readonly approved: boolean;
  /** Whether the tailnet has a service by this name; a machine may announce one nobody created. */
  readonly known: boolean;
}

/**
 * The services the machine actually hosts: the ones it advertises and is approved for. The machines
 * table marks a host with this, so a service's machine is recognisable in the list.
 */
export function hostedServices(node: Node): string[] {
  return node.announcedServices
    .filter((service) => service.active && node.approvedServices.includes(service.name))
    .map((service) => service.name);
}

/** Every service the machine announces or may host, by name. */
export function machineServices(node: Node, services: readonly Service[]): MachineService[] {
  const names = new Set([
    ...node.announcedServices.map((announced) => announced.name),
    ...node.approvedServices,
  ]);

  return [...names].toSorted().map((name) => {
    const announced = node.announcedServices.find((service) => service.name === name);
    const known = services.find((service) => service.name === name);

    return {
      name,
      ports: announced?.ports ?? known?.ports ?? [],
      announced: announced !== undefined,
      active: announced?.active ?? false,
      approved: node.approvedServices.includes(name),
      known: known !== undefined,
    };
  });
}
