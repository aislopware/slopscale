const binaryStep = 1024;
const byteUnits = ["B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"] as const;
/** Below this a value keeps one decimal, so 1.5 GiB does not read as 2 GiB. */
const oneDecimalBelow = 10;

/** A byte count in binary units: "0 B", "512 B", "1.5 KiB", "20 GiB". */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }

  let value = bytes;
  let unit = 0;

  while (value >= binaryStep && unit < byteUnits.length - 1) {
    value /= binaryStep;
    unit += 1;
  }

  const shown = unit === 0 || value >= oneDecimalBelow ? Math.round(value) : value.toFixed(1);

  // Rounding 1023.6 KiB up must not print "1024 KiB"; it is the next unit.
  if (Number(shown) >= binaryStep && unit < byteUnits.length - 1) {
    return `1.0 ${byteUnits[unit + 1] ?? ""}`;
  }

  return `${String(shown)} ${byteUnits[unit] ?? ""}`;
}

/** A rate in binary units per second: "1.5 MiB/s". */
export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** One rate unit for a whole chart, and what a rate in bytes per second is divided by to be in it. */
export interface RateScale {
  readonly unit: string;
  readonly divisor: number;
}

/**
 * The unit the largest rate on a chart reads best in. Every tick is then in it, where formatting
 * each tick on its own gave an axis of "512 KiB/s, 1.0 MiB/s, 1.5 MiB/s".
 */
export function rateScale(peak: number): RateScale {
  let divisor = 1;
  let unit = 0;

  while (peak / divisor >= binaryStep && unit < byteUnits.length - 1) {
    divisor *= binaryStep;
    unit += 1;
  }

  return { unit: `${byteUnits[unit] ?? ""}/s`, divisor };
}

const scaled = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });

/** A value already divided into the scale's unit, with the unit: "2.5 MiB/s". */
export function formatScaled(value: number, scale: RateScale): string {
  return `${scaled.format(value)} ${scale.unit}`;
}

const counts = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 });

/** A count that may run to millions, compact: "950", "12K", "3.4M". */
export function formatCount(count: number): string {
  return counts.format(count);
}

const protocols: Readonly<Record<number, string>> = {
  1: "ICMP",
  6: "TCP",
  17: "UDP",
  47: "GRE",
  50: "ESP",
  58: "ICMPv6",
  132: "SCTP",
  136: "UDP-Lite",
};

/** The IP protocol's name, or its number when it has no common one. */
export function protocolName(proto: number): string {
  return protocols[proto] ?? `IP ${proto}`;
}

/** What a well-known port carries, for the protocols a tailnet mostly sends out. */
const services: Readonly<Record<string, string>> = {
  "6:22": "SSH",
  "6:25": "SMTP",
  "6:53": "DNS",
  "17:53": "DNS",
  "6:80": "HTTP",
  "17:123": "NTP",
  "6:143": "IMAP",
  "6:443": "HTTPS",
  "17:443": "QUIC",
  "6:465": "SMTPS",
  "6:587": "Submission",
  "6:853": "DNS over TLS",
  "17:853": "DNS over QUIC",
  "6:993": "IMAPS",
  "6:1433": "SQL Server",
  "17:3478": "STUN",
  "6:3306": "MySQL",
  "6:3389": "RDP",
  "6:5432": "PostgreSQL",
  "6:6379": "Redis",
  "6:8080": "HTTP",
  "6:8443": "HTTPS",
  "6:9418": "Git",
  "17:41641": "WireGuard",
  "17:51820": "WireGuard",
};

/** The service a port usually carries, or "" when it is not one of the common ones. */
export function serviceName(proto: number, port: number): string {
  return services[`${proto}:${port}`] ?? "";
}

/** "TCP 443", or the protocol alone when it has no ports. */
export function portLabel(proto: number, port: number): string {
  return port === 0 ? protocolName(proto) : `${protocolName(proto)} ${port}`;
}

let regions: Intl.DisplayNames | null = null;

/** A country's name from its ISO 3166 code, in the reader's language; the code when unknown. */
export function countryName(code: string): string {
  if (code === "") {
    return "";
  }

  regions ??= new Intl.DisplayNames(undefined, { type: "region" });

  try {
    return regions.of(code.toUpperCase()) ?? code;
  } catch {
    return code;
  }
}

/** "AS15169 Google LLC", or the number alone when the table has no name for it. */
export function networkLabel(asn: number, name: string): string {
  if (asn === 0) {
    return "";
  }

  return name === "" ? `AS${asn}` : `AS${asn} ${name}`;
}

const registryHandle = /^(?=[^ ]*[A-Z])[A-Z0-9][A-Z0-9._-]*$/u;

/**
 * A network's name split into the organisation and the registry handle it leads with: "VIETEL-AS-AP
 * Viettel Group" is Viettel Group, handle VIETEL-AS-AP. A name that is only a handle,
 * "CLOUDFLARENET", has nothing better to show, so it stays the name.
 */
export function networkName(name: string): { readonly org: string; readonly handle: string } {
  const space = name.indexOf(" ");
  const first = space === -1 ? name : name.slice(0, space);
  const rest = space === -1 ? "" : name.slice(space + 1).trim();

  if (rest === "" || !registryHandle.test(first)) {
    return { org: name, handle: "" };
  }

  return { org: rest, handle: first };
}

const wholePercent = 100;

/** A share of a whole as a whole-number percentage, "<1%" for a sliver that is not nothing. */
export function shareLabel(part: number, whole: number): string {
  if (whole <= 0 || part <= 0) {
    return "0%";
  }

  const percent = (part / whole) * wholePercent;

  return percent < 1 ? "<1%" : `${Math.round(percent)}%`;
}
