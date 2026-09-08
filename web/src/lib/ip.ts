/**
 * IP address checks that accept exactly what Go's netip.ParseAddr accepts, so the console never
 * marks an address wrong that the server would take: four decimal octets without leading zeros, or
 * up to eight hex groups with one "::" and an IPv4 address allowed in the last two groups.
 */

const octet = /^(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/v;
const hexGroup = /^[0-9a-f]{1,4}$/iv;
const ipv4Octets = 4;
const ipv6Groups = 8;
/** An embedded IPv4 address fills the last two 16-bit groups. */
const embeddedIpv4Groups = 2;

export function isIpv4(text: string): boolean {
  const parts = text.split(".");

  return parts.length === ipv4Octets && parts.every((part) => octet.test(part));
}

/**
 * Counts the 16-bit groups on one side of "::", or null when a piece is not hex. An embedded IPv4
 * address counts as two groups and is only allowed at the very end of the address.
 */
function groupCount(side: string, last: boolean): number | null {
  if (side === "") {
    return 0;
  }

  const pieces = side.split(":");
  let count = 0;

  for (const [index, piece] of pieces.entries()) {
    if (hexGroup.test(piece)) {
      count += 1;
    } else if (last && index === pieces.length - 1 && isIpv4(piece)) {
      count += embeddedIpv4Groups;
    } else {
      return null;
    }
  }

  return count;
}

export function isIpv6(text: string): boolean {
  const halves = text.split("::");

  if (halves.length > 2) {
    return false;
  }

  const [head = "", tail] = halves;

  if (tail === undefined) {
    return groupCount(head, true) === ipv6Groups;
  }

  const before = groupCount(head, false);
  const after = groupCount(tail, true);

  // "::" stands for at least one group of zeros, so both sides together leave room for it.
  return before !== null && after !== null && before + after < ipv6Groups;
}

/** Whether the text is an IPv4 or IPv6 address, without a zone. */
export function isIp(text: string): boolean {
  return isIpv4(text) || isIpv6(text);
}
