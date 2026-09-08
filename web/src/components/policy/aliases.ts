import { isIp, isIpv4, isIpv6 } from "~/lib/ip.ts";

/**
 * What a string in a policy may name, as `hscontrol/policy/v2` classifies it: an address or a
 * range, the wildcard, a user, a group, a tag, an autogroup or a host from the hosts section.
 */
export type AliasKind = "prefix" | "wildcard" | "user" | "group" | "tag" | "autogroup" | "host";

const ipv4Bits = 32;
const ipv6Bits = 128;
/** A prefix length as netip.ParsePrefix reads it: decimal, no sign, no leading zero. */
const prefixBits = /^(?:0|[1-9]\d{0,2})$/v;

/**
 * Whether the text is an IPv4 or IPv6 address as netip.ParseAddr reads it, which allows a zone
 * ("fe80::1%eth0") after an IPv6 address.
 */
export function isAddress(text: string): boolean {
  const [address = "", zone, ...rest] = text.split("%");

  if (rest.length > 0 || zone === "") {
    return false;
  }

  return zone === undefined ? isIp(address) : isIpv6(address);
}

/** Whether the text is an address or a CIDR range, as Go's netip parses them; a range takes no zone. */
export function isPrefix(text: string): boolean {
  const slash = text.indexOf("/");

  if (slash === -1) {
    return isAddress(text);
  }

  const address = text.slice(0, slash);
  const bits = text.slice(slash + 1);

  if (!prefixBits.test(bits)) {
    return false;
  }

  if (isIpv4(address)) {
    return Number(bits) <= ipv4Bits;
  }

  return isIpv6(address) && Number(bits) <= ipv6Bits;
}

/** Whether the range is 0.0.0.0/0 or ::/0, which a grant must write as "*" or autogroup:internet. */
export function isDefaultRoute(text: string): boolean {
  return text.endsWith("/0") && isPrefix(text);
}

export function aliasKind(text: string): AliasKind | null {
  if (isPrefix(text)) {
    return "prefix";
  }

  if (text === "*") {
    return "wildcard";
  }

  if (text.includes("@")) {
    return "user";
  }

  if (text.startsWith("group:")) {
    return "group";
  }

  if (text.startsWith("tag:")) {
    return "tag";
  }

  if (text.startsWith("autogroup:")) {
    return "autogroup";
  }

  return text.includes(":") ? null : "host";
}

export const roleAutogroups = [
  "autogroup:owner",
  "autogroup:admin",
  "autogroup:network-admin",
  "autogroup:it-admin",
  "autogroup:auditor",
] as const;

export const autogroups = [
  "autogroup:internet",
  "autogroup:member",
  "autogroup:nonroot",
  "autogroup:tagged",
  "autogroup:self",
  "autogroup:danger-all",
  "autogroup:shared",
  ...roleAutogroups,
] as const;

export type Autogroup = (typeof autogroups)[number];

/** Where an alias stands, which decides which autogroups may stand there. */
export type Side = "src" | "dst" | "sshSrc" | "sshDst" | "nodeAttrs";

export const autogroupsFor: Readonly<Record<Side, readonly Autogroup[]>> = {
  src: [
    "autogroup:member",
    "autogroup:tagged",
    "autogroup:danger-all",
    "autogroup:shared",
    ...roleAutogroups,
  ],
  dst: [
    "autogroup:internet",
    "autogroup:member",
    "autogroup:tagged",
    "autogroup:self",
    ...roleAutogroups,
  ],
  sshSrc: ["autogroup:member", "autogroup:tagged", "autogroup:shared", ...roleAutogroups],
  sshDst: ["autogroup:member", "autogroup:tagged", "autogroup:self", ...roleAutogroups],
  nodeAttrs: ["autogroup:member", "autogroup:tagged", ...roleAutogroups],
};

const sideLabels: Readonly<Record<Side, string>> = {
  src: "a source",
  dst: "a destination",
  sshSrc: "an SSH source",
  sshDst: "an SSH destination",
  nodeAttrs: "a nodeAttrs target",
};

export function isAutogroup(text: string): text is Autogroup {
  return autogroups.some((known) => known === text);
}

const sideBound: Readonly<Partial<Record<Autogroup, string>>> = {
  "autogroup:internet": "autogroup:internet can only be a destination",
  "autogroup:self": "autogroup:self can only be a destination",
  "autogroup:shared": "autogroup:shared can only be a source",
  "autogroup:danger-all": "autogroup:danger-all can only be a source",
  "autogroup:nonroot": "autogroup:nonroot is an SSH user, not a machine",
};

/** Why the autogroup cannot stand on that side, or null when it can. */
export function autogroupIssue(text: string, side: Side): string | null {
  if (!isAutogroup(text)) {
    return `Unknown autogroup "${text}"`;
  }

  if (autogroupsFor[side].includes(text)) {
    return null;
  }

  return sideBound[text] ?? `${text} cannot be ${sideLabels[side]}`;
}

export const protocols = [
  "icmp",
  "igmp",
  "ipv4",
  "ip-in-ip",
  "tcp",
  "egp",
  "igp",
  "udp",
  "gre",
  "esp",
  "ah",
  "ipv6-icmp",
  "sctp",
  "fc",
] as const;

const protocolMax = 255;

/** Why the protocol is not one the server knows, or null when it is. */
export function protocolIssue(text: string): string | null {
  if (text === "" || protocols.some((known) => known === text)) {
    return null;
  }

  if (text === "*") {
    return 'Protocol "*" is not allowed; leave proto out to match every protocol';
  }

  if (!/^\d+$/v.test(text)) {
    return `Unknown protocol "${text}"; use a name such as tcp or a number from 0 to 255`;
  }

  if (text === "0" || text.startsWith("0")) {
    return "A protocol number cannot start with 0";
  }

  return Number(text) > protocolMax ? "A protocol number goes up to 255" : null;
}

const portMax = 65_535;

function portIssue(text: string): string | null {
  const [first, last, extra] = text.split("-");

  if (extra !== undefined || first === undefined || !/^\d+$/v.test(first)) {
    return `"${text}" is not a port or a range such as 8000-8080`;
  }

  if (last !== undefined && !/^\d+$/v.test(last)) {
    return `"${text}" is not a port or a range such as 8000-8080`;
  }

  if (Number(first) > portMax || (last !== undefined && Number(last) > portMax)) {
    return "Ports go up to 65535";
  }

  return last !== undefined && Number(last) < Number(first)
    ? `Range "${text}" ends before it starts`
    : null;
}

/** Why the ports are not "*", a port, a range or a comma list of those, or null when they are. */
export function portsIssue(text: string): string | null {
  if (text === "*") {
    return null;
  }

  for (const part of text.split(",")) {
    const issue = portIssue(part);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

/** Why a grant "ip" entry ("*", "443", "tcp:22,80") is malformed, or null when it is fine. */
export function ipEntryIssue(text: string): string | null {
  if (text === "*") {
    return null;
  }

  const colon = text.indexOf(":");

  if (colon === -1) {
    return portsIssue(text);
  }

  const protocol = text.slice(0, colon);
  const ports = text.slice(colon + 1);

  if (ports.includes(":")) {
    return `"${text}" has more than one colon; write proto:ports such as tcp:443`;
  }

  return protocolIssue(protocol) ?? portsIssue(ports);
}

/** Splits an ACL destination "alias:ports" at the ports; null when there is no port part. */
export function splitPorts(
  text: string,
): { readonly alias: string; readonly ports: string } | null {
  const colon = text.lastIndexOf(":");

  if (colon === -1) {
    return null;
  }

  const alias = text.slice(0, colon);
  const ports = text.slice(colon + 1);

  // An IPv6 address holds colons of its own; its ports come after the last one all the same, but
  // only when what is left is still an address.
  return alias === "" || (alias.includes(":") && aliasKind(alias) === null && !isPrefix(alias))
    ? null
    : { alias, ports };
}

/** A Go duration such as 12h, 30m or 1h30m, which checkPeriod takes, or the word always. */
export function isCheckPeriod(text: string): boolean {
  return text === "always" || /^(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h))+$/v.test(text);
}
