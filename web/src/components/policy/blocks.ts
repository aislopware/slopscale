/** The names a policy draft mentions, for the building blocks panel. */
export interface PolicyBlocks {
  readonly groups: readonly string[];
  readonly tags: readonly string[];
  readonly autogroups: readonly string[];
  /** False when the draft is not valid HuJSON, so the names come from a plain text scan. */
  readonly valid: boolean;
}

/** A string literal or a comment. Strings are kept verbatim so their contents survive. */
const commentOrString = /"(?:\\.|[^"\\])*"|\/\/[^\n]*|\/\*[\s\S]*?\*\//gu;
/** A string literal or a comma that closes nothing: HuJSON's trailing comma. */
const stringOrTrailingComma = /"(?:\\.|[^"\\])*"|,(?=\s*[}\]])/gu;
/** `group:x`, `tag:x`, `autogroup:x`; a `:port` or `:*` suffix ends the match. */
const namePattern = /(?:autogroup|group|tag):[\w.@-]+/gu;

const groupPrefix = "group:";
const tagPrefix = "tag:";
const autogroupPrefix = "autogroup:";

const quote = '"';

function withoutComments(match: string): string {
  return match.startsWith(quote) ? match : " ";
}

function withoutTrailingCommas(match: string): string {
  return match.startsWith(quote) ? match : "";
}

/** HuJSON as JSON: comments blanked, trailing commas dropped, string contents untouched. */
export function toJson(text: string): string {
  return text
    .replaceAll(commentOrString, withoutComments)
    .replaceAll(stringOrTrailingComma, withoutTrailingCommas);
}

/** Every string and object key in the parsed document, so a name counts wherever it appears. */
function collect(value: unknown, into: string[]): void {
  if (typeof value === "string") {
    into.push(value);

    return;
  }

  if (typeof value === "object" && value !== null) {
    // Arrays land here too: their indices are keys that no name pattern matches.
    for (const [key, item] of Object.entries(value)) {
      into.push(key);
      collect(item, into);
    }
  }
}

function parsedStrings(json: string): string[] | null {
  try {
    const document: unknown = JSON.parse(json);
    const strings: string[] = [];

    collect(document, strings);

    return strings;
  } catch {
    return null;
  }
}

function namesIn(texts: readonly string[]): string[] {
  const found = new Set<string>();

  for (const text of texts) {
    for (const match of text.matchAll(namePattern)) {
      found.add(match[0]);
    }
  }

  return [...found].toSorted((left, right) => left.localeCompare(right));
}

const emptyBlocks: PolicyBlocks = { groups: [], tags: [], autogroups: [], valid: true };

/**
 * Reads the groups, tags and autogroups out of a policy draft. HuJSON is turned into JSON and
 * parsed; when the draft is mid-edit and does not parse, the same names are scanned out of the
 * text, so the panel keeps up with every keystroke instead of blanking on a missing brace.
 */
export function policyBlocks(text: string): PolicyBlocks {
  if (text.trim() === "") {
    return emptyBlocks;
  }

  const json = toJson(text);
  const strings = parsedStrings(json);
  const names = namesIn(strings ?? [json]);

  return {
    groups: names.filter((name) => name.startsWith(groupPrefix)),
    tags: names.filter((name) => name.startsWith(tagPrefix)),
    autogroups: names.filter((name) => name.startsWith(autogroupPrefix)),
    valid: strings !== null,
  };
}
