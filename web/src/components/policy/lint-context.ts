import { aliasKind, autogroupIssue } from "~/components/policy/aliases.ts";
import type { AliasKind, Side } from "~/components/policy/aliases.ts";
import type { KeyInfo } from "~/components/policy/schema.ts";
import type { JsonArray, JsonNode, JsonObject, JsonString, Span } from "~/lib/hujson/ast.ts";

export interface Problem extends Span {
  readonly severity: "error" | "warning";
  readonly message: string;
}

/** The names a policy defines, so a reference to one can be checked wherever it appears. */
export interface Names {
  readonly groups: ReadonlySet<string>;
  readonly tags: ReadonlySet<string>;
  readonly hosts: ReadonlySet<string>;
  readonly postures: ReadonlySet<string>;
}

const kindLabels: Readonly<Record<AliasKind, string>> = {
  prefix: "an address or range",
  wildcard: "*",
  user: "a user",
  group: "a group",
  tag: "a tag",
  autogroup: "an autogroup",
  host: "a host",
};

const sideLabels: Readonly<Record<Side, string>> = {
  src: "A source",
  dst: "A destination",
  sshSrc: "An SSH source",
  sshDst: "An SSH destination",
  nodeAttrs: "A nodeAttrs target",
};

function aliasHint(text: string): string {
  if (text === "") {
    return "An empty string names nothing";
  }

  if (text.startsWith("posture:")) {
    return "Postures go in srcPosture, not among the sources";
  }

  return `"${text}" is not a user, group, tag, autogroup, host, address or *`;
}

export const anyAlias: readonly AliasKind[] = [
  "prefix",
  "wildcard",
  "user",
  "group",
  "tag",
  "autogroup",
  "host",
];

/** Where a list of aliases stands and which kinds may stand there. */
export interface AliasPlace {
  readonly side: Side;
  readonly allowed: readonly AliasKind[];
  /** What to call the list in a message. */
  readonly what: string;
}

/** Collects problems while walking the tree; the section functions all report through it. */
export class Lint {
  readonly problems: Problem[] = [];
  readonly names: Names;

  constructor(names: Names) {
    this.names = names;
  }

  error(span: Span, message: string): void {
    this.problems.push({ from: span.from, to: span.to, severity: "error", message });
  }

  warn(span: Span, message: string): void {
    this.problems.push({ from: span.from, to: span.to, severity: "warning", message });
  }

  /** The node as an object, or null after reporting what it is instead. */
  object(node: JsonNode, what: string): JsonObject | null {
    if (node.kind === "object") {
      return node;
    }

    if (node.kind !== "error") {
      this.error(node, `${what} must be an object`);
    }

    return null;
  }

  array(node: JsonNode, what: string): JsonArray | null {
    if (node.kind === "array") {
      return node;
    }

    if (node.kind !== "error") {
      this.error(node, `${what} must be a list`);
    }

    return null;
  }

  /** The strings in a list, reporting the items that are not strings. */
  strings(node: JsonNode, what: string): JsonString[] {
    const array = this.array(node, what);

    if (array === null) {
      return node.kind === "string" ? [node] : [];
    }

    const strings: JsonString[] = [];

    for (const item of array.items) {
      if (item.kind === "string") {
        strings.push(item);
      } else if (item.kind !== "error") {
        this.error(item, `${what} holds strings`);
      }
    }

    return strings;
  }

  bool(node: JsonNode, what: string): void {
    if (node.kind !== "bool" && node.kind !== "error") {
      this.error(node, `${what} is true or false`);
    }
  }

  /**
   * Reports keys the object may not hold and keys that appear twice. Keys starting with # are
   * metadata other tools leave in ACLs, which the server ignores there and nowhere else.
   */
  keys(object: JsonObject, known: readonly KeyInfo[], what: string): void {
    const seen = new Set<string>();
    const hashOk = what === "an ACL" || what === "a grant";

    for (const { key } of object.entries) {
      if (seen.has(key.name)) {
        this.error(key, `"${key.name}" appears twice in ${what}`);
      }

      seen.add(key.name);

      const metadata = hashOk && key.name.startsWith("#");

      if (!metadata && !known.some((info) => info.name === key.name)) {
        this.error(key, `Unknown key "${key.name}" in ${what}`);
      }
    }
  }

  /** Checks one alias string against the kinds allowed on that side and the names it refers to. */
  alias(node: JsonString, place: Omit<AliasPlace, "what">): AliasKind | null {
    const text = node.value;
    const kind = aliasKind(text);

    if (text === "" || kind === null) {
      this.error(node, aliasHint(text));

      return null;
    }

    if (!place.allowed.includes(kind)) {
      this.error(node, `${sideLabels[place.side]} cannot be ${kindLabels[kind]}`);

      return kind;
    }

    this.reference(node, kind, place.side);

    return kind;
  }

  private reference(node: JsonString, kind: AliasKind, side: Side): void {
    const text = node.value;

    if (kind === "group" && !this.names.groups.has(text)) {
      this.error(node, `"${text}" is not defined in groups`);
    } else if (kind === "tag" && !this.names.tags.has(text)) {
      this.error(node, `"${text}" has no owners in tagOwners`);
    } else if (kind === "host" && !this.names.hosts.has(text)) {
      this.error(node, `"${text}" is not defined in hosts`);
    } else if (kind === "autogroup") {
      const issue = autogroupIssue(text, side);

      if (issue !== null) {
        this.error(node, issue);
      }
    }
  }

  aliases(node: JsonNode, place: AliasPlace): JsonString[] {
    const strings = this.strings(node, place.what);

    for (const item of strings) {
      this.alias(item, place);
    }

    return strings;
  }

  postureRefs(node: JsonNode, what: string): void {
    for (const item of this.strings(node, what)) {
      if (!item.value.startsWith("posture:")) {
        this.error(item, `${what} names postures, such as "posture:office"`);
      } else if (!this.names.postures.has(item.value)) {
        this.error(item, `"${item.value}" is not defined in postures`);
      }
    }
  }
}
