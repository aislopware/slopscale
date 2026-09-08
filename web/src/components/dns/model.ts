import type { Dns } from "~/api/queries.ts";
import type { DnsRecord, DnsSettings } from "~/api/schema.gen.ts";
import { isIp, isIpv4, isIpv6 } from "~/lib/ip.ts";

export type RecordType = DnsRecord["type"];

export const recordTypes: readonly RecordType[] = ["", "A", "AAAA"];

export function recordTypeLabel(type: RecordType): string {
  return type === "" ? "Auto (A or AAAA)" : type;
}

type SplitMap = DnsSettings["splitNameservers"];

/** The split map without the nulls the schema allows, in domain order. */
export function splitEntries(settings: DnsSettings): readonly (readonly [string, string[]])[] {
  return entriesOf(settings.splitNameservers);
}

function entriesOf(map: SplitMap): readonly (readonly [string, string[]])[] {
  return Object.entries(map)
    .flatMap(([domain, servers]): (readonly [string, string[]])[] =>
      servers === null ? [] : [[domain, servers]],
    )
    .toSorted(([left], [right]) => left.localeCompare(right));
}

function cloneSplit(map: SplitMap): Record<string, string[]> {
  return Object.fromEntries(entriesOf(map).map(([domain, servers]) => [domain, [...servers]]));
}

/** Whether the machine keeps using the nameserver while it has an exit node selected. */
export function keptWithExitNode(settings: DnsSettings, nameserver: string): boolean {
  return settings.useWithExitNode.includes(nameserver);
}

/**
 * Whether the domain's resolvers survive an exit node. The client keeps the route only when every
 * one of them is marked, so the console marks them all or none.
 */
export function splitKeptWithExitNode(settings: DnsSettings, domain: string): boolean {
  const servers = settings.splitNameservers[domain];
  const kept = settings.splitUseWithExitNode[domain];

  if (servers === undefined || servers === null || servers.length === 0 || !kept) {
    return false;
  }

  return servers.every((server) => kept.includes(server));
}

/**
 * What an editor may send back: the effective settings without the extra records when
 * dns.extra_records_path owns them, since the server refuses a PUT that carries records while the
 * file is in charge.
 */
export function editableSettings(dns: Dns): DnsSettings {
  const settings = cloneSettings(dns.effective);

  if (dns.extraRecordsPath !== "") {
    settings.extraRecords = [];
  }

  return settings;
}

/** A copy every editor starts from, so a PUT always carries the whole configuration. */
export function cloneSettings(settings: DnsSettings): DnsSettings {
  return {
    nameservers: [...settings.nameservers],
    overrideLocalDns: settings.overrideLocalDns,
    splitNameservers: cloneSplit(settings.splitNameservers),
    useWithExitNode: [...settings.useWithExitNode],
    splitUseWithExitNode: cloneSplit(settings.splitUseWithExitNode),
    searchDomains: [...settings.searchDomains],
    extraRecords: settings.extraRecords.map((record) => ({ ...record })),
  };
}

const maxPort = 65_535;
const label = /^[a-z0-9_](?:[a-z0-9_\-]{0,61}[a-z0-9_])?$/v;
const maxDomainLength = 253;

function isPort(value: string): boolean {
  const port = Number(value);

  return Number.isInteger(port) && port >= 1 && port <= maxPort;
}

function isUrl(value: string): boolean {
  try {
    const url = new URL(value);

    return url.protocol === "https:" && url.hostname !== "";
  } catch {
    return false;
  }
}

/** Splits "host:port" and "[v6]:port" into their parts, or null when the value has no port. */
function hostAndPort(value: string): readonly [string, string] | null {
  if (value.startsWith("[")) {
    const end = value.indexOf("]:");

    return end === -1 ? null : [value.slice(1, end), value.slice(end + "]:".length)];
  }

  const colon = value.indexOf(":");

  if (colon === -1 || colon !== value.lastIndexOf(":")) {
    return null;
  }

  return [value.slice(0, colon), value.slice(colon + 1)];
}

/**
 * The shape the server accepts as a resolver: an IP, an IP with port, or an https:// URL. Whether
 * the client knows the DoH provider is checked by the server, which answers 400 for one it does
 * not.
 */
export function isNameserver(value: string): boolean {
  if (isIp(value) || isUrl(value)) {
    return true;
  }

  const parts = hostAndPort(value);

  return parts !== null && isIp(parts[0]) && isPort(parts[1]);
}

export function isDomain(value: string): boolean {
  return (
    value !== "" &&
    value.length <= maxDomainLength &&
    value.split(".").every((part) => label.test(part))
  );
}

export function normalizeDomain(value: string): string {
  return value.trim().toLowerCase().replace(/\.$/v, "");
}

/** One entry per line or comma, trimmed, empty ones dropped. */
export function parseList(text: string): string[] {
  return text
    .split(/[\n,]/v)
    .map((part) => part.trim())
    .filter((part) => part !== "");
}

export function nameserverError(value: string): string | null {
  if (value === "") {
    return "Enter a nameserver.";
  }

  return isNameserver(value)
    ? null
    : "Use an IP address, an IP with port, or the https:// URL of a known DNS-over-HTTPS provider.";
}

export function nameserversError(values: readonly string[]): string | null {
  if (values.length === 0) {
    return "Enter at least one nameserver.";
  }

  const bad = values.find((value) => !isNameserver(value));

  return bad === undefined ? null : `"${bad}" is not a nameserver.`;
}

export function domainError(value: string): string | null {
  if (value === "") {
    return "Enter a domain.";
  }

  return isDomain(value) ? null : "Use a domain such as corp.example.com.";
}

/** Why a record cannot be saved, or null when it can. */
export function recordError(record: DnsRecord): string | null {
  const name = normalizeDomain(record.name);

  if (name === "") {
    return "Enter a name.";
  }

  if (!isDomain(name)) {
    return "Use a full name such as grafana.example.com.";
  }

  const value = record.value.trim();

  if (value === "") {
    return "Enter a value.";
  }

  switch (record.type) {
    case "": {
      return isIp(value) ? null : "The value must be an IP address.";
    }
    case "A": {
      return isIpv4(value) ? null : "An A record needs an IPv4 address.";
    }
    case "AAAA": {
      return isIpv6(value) ? null : "An AAAA record needs an IPv6 address.";
    }
    default: {
      return null;
    }
  }
}

export function withNameserver(settings: DnsSettings, value: string): DnsSettings {
  const next = cloneSettings(settings);

  if (!next.nameservers.includes(value)) {
    next.nameservers.push(value);
  }

  return next;
}

export function withoutNameserver(settings: DnsSettings, value: string): DnsSettings {
  const next = cloneSettings(settings);

  next.nameservers = next.nameservers.filter((existing) => existing !== value);
  next.useWithExitNode = next.useWithExitNode.filter((existing) => existing !== value);

  return next;
}

/**
 * Turning the override off also drops the nameservers kept with an exit node: the client honours
 * the flag only on the resolvers it uses for every query, and the server refuses the pair.
 */
export function withOverrideLocalDns(settings: DnsSettings, on: boolean): DnsSettings {
  const next = cloneSettings(settings);

  next.overrideLocalDns = on;

  if (!on) {
    next.useWithExitNode = [];
  }

  return next;
}

export function withUseWithExitNode(
  settings: DnsSettings,
  nameserver: string,
  on: boolean,
): DnsSettings {
  const next = cloneSettings(settings);

  next.useWithExitNode = next.useWithExitNode.filter((existing) => existing !== nameserver);

  if (on) {
    next.useWithExitNode.push(nameserver);
  }

  return next;
}

/** Marks every resolver of the domain, or none, since the client needs all of them. */
export function withSplitUseWithExitNode(
  settings: DnsSettings,
  domain: string,
  on: boolean,
): DnsSettings {
  const next = cloneSettings(settings);
  const servers = next.splitNameservers[domain];

  if (on && servers !== undefined && servers !== null) {
    next.splitUseWithExitNode[domain] = [...servers];
  } else {
    next.splitUseWithExitNode = Object.fromEntries(
      Object.entries(next.splitUseWithExitNode).filter(([existing]) => existing !== domain),
    );
  }

  return next;
}

export interface SplitEdit {
  readonly domain: string;
  readonly servers: readonly string[];
  /** The domain being renamed, which is dropped. */
  readonly previous?: string | undefined;
}

/** Sets the domain's resolvers; a domain kept with an exit node keeps its new resolvers too. */
export function withSplit(settings: DnsSettings, edit: SplitEdit): DnsSettings {
  const previous = edit.previous ?? edit.domain;
  const kept = splitKeptWithExitNode(settings, previous);
  const next =
    previous === edit.domain ? cloneSettings(settings) : withoutSplit(settings, previous);

  next.splitNameservers[edit.domain] = [...edit.servers];

  return withSplitUseWithExitNode(next, edit.domain, kept);
}

export function withoutSplit(settings: DnsSettings, domain: string): DnsSettings {
  const next = withSplitUseWithExitNode(settings, domain, false);

  next.splitNameservers = Object.fromEntries(
    Object.entries(next.splitNameservers).filter(([existing]) => existing !== domain),
  );

  return next;
}

export function withSearchDomain(settings: DnsSettings, domain: string): DnsSettings {
  const next = cloneSettings(settings);

  if (!next.searchDomains.includes(domain)) {
    next.searchDomains.push(domain);
  }

  return next;
}

export function withoutSearchDomain(settings: DnsSettings, domain: string): DnsSettings {
  const next = cloneSettings(settings);

  next.searchDomains = next.searchDomains.filter((existing) => existing !== domain);

  return next;
}

/** Adds a record, or replaces the one at `index`. */
export function withRecord(settings: DnsSettings, record: DnsRecord, index?: number): DnsSettings {
  const next = cloneSettings(settings);
  const clean = { ...record, name: normalizeDomain(record.name), value: record.value.trim() };

  if (index === undefined) {
    next.extraRecords.push(clean);
  } else {
    next.extraRecords[index] = clean;
  }

  return next;
}

export function withoutRecord(settings: DnsSettings, index: number): DnsSettings {
  const next = cloneSettings(settings);

  next.extraRecords.splice(index, 1);

  return next;
}
