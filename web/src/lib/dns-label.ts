const maxLabelLength = 63;

/**
 * Why a name cannot be a machine's DNS label, in the server's words (tailscale's
 * dnsname.ValidLabel), or null when it can. The server is the authority; this only spares a round
 * trip.
 */
export function dnsLabelIssue(label: string): string | null {
  if (label === "") {
    return "empty DNS label";
  }

  if (label.length > maxLabelLength) {
    return "DNS label is longer than 63 characters";
  }

  if (!isAlphanumeric(label.at(0) ?? "")) {
    return "must start with a letter or number";
  }

  if (!isAlphanumeric(label.at(-1) ?? "")) {
    return "must end with a letter or number";
  }

  for (const character of label) {
    if (!isAlphanumeric(character) && character !== "-") {
      return `contains invalid character ${JSON.stringify(character)}`;
    }
  }

  return null;
}

function isAlphanumeric(character: string): boolean {
  return /^[A-Za-z0-9]$/u.test(character);
}
