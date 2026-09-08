/** A piece of a URL and what it is, so a display can weight the host over the rest. */
export interface UrlPart {
  readonly kind: "scheme" | "host" | "port" | "path" | "query" | "hash" | "text";
  readonly text: string;
}

const mailto = "mailto:";

/**
 * Splits a URL the way an address bar does: the scheme, the host, then the port, path, query and
 * fragment. A mailto: address is the scheme and the recipients. Anything that is not a URL comes
 * back whole as text, so a caller can always render the parts.
 */
export function urlParts(url: string): UrlPart[] {
  if (url.startsWith(mailto)) {
    return [
      { kind: "scheme", text: mailto },
      { kind: "host", text: url.slice(mailto.length) },
    ];
  }

  let parsed: URL;

  try {
    parsed = new URL(url);
  } catch {
    return [{ kind: "text", text: url }];
  }

  if (parsed.host === "") {
    return [{ kind: "text", text: url }];
  }

  const hostStart = url.indexOf(parsed.host, parsed.protocol.length);

  if (hostStart === -1) {
    return [{ kind: "text", text: url }];
  }

  // The parts after the host are taken from the text as written rather than from the parsed URL,
  // which normalises them: a trailing slash or an encoded space must show as the operator typed it.
  const rest = url.slice(hostStart + parsed.hostname.length);
  const parts: UrlPart[] = [
    { kind: "scheme", text: url.slice(0, hostStart) },
    { kind: "host", text: parsed.hostname },
  ];
  const match = /^(?<port>:\d+)?(?<path>[^?#]*)(?<query>\?[^#]*)?(?<hash>#.*)?$/sv.exec(rest);

  if (match?.groups === undefined) {
    parts.push({ kind: "path", text: rest });

    return parts;
  }

  for (const kind of ["port", "path", "query", "hash"] as const) {
    const text = match.groups[kind];

    if (text !== undefined && text !== "") {
      parts.push({ kind, text });
    }
  }

  return parts;
}
