import type { DnsRecord, DnsSettings } from "~/api/schema.gen.ts";

export type RecordType = DnsRecord["type"];

export const recordTypes: readonly RecordType[] = ["", "A", "AAAA", "TXT"];

export function recordTypeLabel(type: RecordType): string {
  return type === "" ? "Auto (A or AAAA)" : type;
}

/** The split map without the nulls the schema allows, in domain order. */
export function splitEntries(settings: DnsSettings): readonly (readonly [string, string[]])[] {
  return Object.entries(settings.splitNameservers)
    .flatMap(([domain, servers]): (readonly [string, string[]])[] =>
      servers === null ? [] : [[domain, servers]],
    )
    .toSorted(([left], [right]) => left.localeCompare(right));
}

/** A copy every editor starts from, so a PUT always carries the whole configuration. */
export function cloneSettings(settings: DnsSettings): DnsSettings {
  return {
    nameservers: [...settings.nameservers],
    overrideLocalDns: settings.overrideLocalDns,
    splitNameservers: Object.fromEntries(
      splitEntries(settings).map(([domain, servers]) => [domain, [...servers]]),
    ),
    searchDomains: [...settings.searchDomains],
    extraRecords: settings.extraRecords.map((record) => ({ ...record })),
  };
}

const octet = /^\d{1,3}$/v;
const hexGroup = /^[0-9a-f]{1,4}$/iv;
const maxIpv6Groups = 8;
const maxOctet = 255;
const maxPort = 65_535;
const label = /^[a-z0-9_](?:[a-z0-9_\-]{0,61}[a-z0-9_])?$/v;
const maxDomainLength = 253;
const ipv4Parts = 4;

export function isIpv4(value: string): boolean {
  const parts = value.split(".");

  return (
    parts.length === ipv4Parts &&
    parts.every((part) => octet.test(part) && Number(part) <= maxOctet)
  );
}

export function isIpv6(value: string): boolean {
  const halves = value.split("::");

  if (halves.length > 2 || !value.includes(":")) {
    return false;
  }

  const groups = halves.flatMap((half) => (half === "" ? [] : half.split(":")));

  if (!groups.every((group) => hexGroup.test(group))) {
    return false;
  }

  return halves.length === 2 ? groups.length < maxIpv6Groups : groups.length === maxIpv6Groups;
}

export function isIp(value: string): boolean {
  return isIpv4(value) || isIpv6(value);
}

function isPort(value: string): boolean {
  const port = Number(value);

  return Number.isInteger(port) && port >= 1 && port <= maxPort;
}

function isUrl(value: string): boolean {
  try {
    const url = new URL(value);

    return (url.protocol === "https:" || url.protocol === "tls:") && url.hostname !== "";
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

/** What the server accepts as a resolver: an IP, an IP with port, or a DoH/DoT URL. */
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
    : "Use an IP address, an IP with port, or an https:// or tls:// URL.";
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
    case "TXT": {
      return null;
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

  return next;
}

export function withOverrideLocalDns(settings: DnsSettings, on: boolean): DnsSettings {
  return { ...cloneSettings(settings), overrideLocalDns: on };
}

export interface SplitEdit {
  readonly domain: string;
  readonly servers: readonly string[];
  /** The domain being renamed, which is dropped. */
  readonly previous?: string | undefined;
}

/** Sets the domain's resolvers. */
export function withSplit(settings: DnsSettings, edit: SplitEdit): DnsSettings {
  const next =
    edit.previous === undefined || edit.previous === edit.domain
      ? cloneSettings(settings)
      : withoutSplit(settings, edit.previous);

  next.splitNameservers[edit.domain] = [...edit.servers];

  return next;
}

export function withoutSplit(settings: DnsSettings, domain: string): DnsSettings {
  const next = cloneSettings(settings);

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
