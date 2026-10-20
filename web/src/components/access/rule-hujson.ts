import type { Group, Posture } from "~/api/queries.ts";
import { hasPorts, isSelf } from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";
import type { RuleDraft } from "~/components/access/rule-draft.ts";
import { ipEntryIssue } from "~/components/policy/aliases.ts";
import type { EditorProblem } from "~/lib/editor/problems.ts";
import { parseHujson } from "~/lib/hujson/ast.ts";
import type { JsonNode, JsonObject, Span } from "~/lib/hujson/ast.ts";

/**
 * A rule's conditions as the grant the server compiles them to, so the operator can read and edit
 * the rule the way the policy file is written. The server names a group by its id (`accessgroup:3`)
 * and a posture as `posture:#3`; the snippet names both by their unique names so it reads as the
 * pages do, and parsing turns the names back into ids.
 */

/** The fields of a draft the snippet carries: the If column. */
export type RuleConditionsPatch = Pick<
  RuleDraft,
  "sources" | "destinations" | "postures" | "protocol" | "ports"
>;

const groupPrefix = "accessgroup:";
const posturePrefix = "posture:#";
const selfAlias = "autogroup:self";
const snippetKeys = ["src", "dst", "ip", "srcPosture"] as const;

function groupAlias(groups: readonly Group[], id: string): string {
  const group = groups.find((candidate) => candidate.id === id);

  if (group !== undefined && isSelf(group)) {
    return selfAlias;
  }

  return `${groupPrefix}${group?.name ?? id}`;
}

function postureAlias(postures: readonly Posture[], id: string): string {
  return `${posturePrefix}${postures.find((posture) => posture.id === id)?.name ?? id}`;
}

/** The grant's "ip" entries, as the server renders the rule's protocol and ports. */
function ipEntries(protocol: Protocol, ports: string): string[] {
  if (protocol === "icmp") {
    return ["icmp:*", "ipv6-icmp:*"];
  }

  if (hasPorts(protocol)) {
    const list = ports.replaceAll(/\s+/gv, "");

    return [`${protocol}:${list === "" ? "*" : list}`];
  }

  return ["*"];
}

function line(key: string, values: readonly string[]): string {
  return `  ${JSON.stringify(key)}: [${values.map((value) => JSON.stringify(value)).join(", ")}]`;
}

/** The draft's conditions as one HuJSON grant. */
export function ruleSnippet(
  draft: RuleDraft,
  groups: readonly Group[],
  postures: readonly Posture[],
): string {
  const lines = [
    line(
      "src",
      draft.sources.map((id) => groupAlias(groups, id)),
    ),
    line(
      "dst",
      draft.destinations.map((id) => groupAlias(groups, id)),
    ),
    line("ip", ipEntries(draft.protocol, draft.ports)),
    ...(draft.postures.length === 0
      ? []
      : [
          line(
            "srcPosture",
            draft.postures.map((id) => postureAlias(postures, id)),
          ),
        ]),
  ];

  return `{\n${lines.join(",\n")},\n}`;
}

export type SnippetResult =
  | { readonly ok: true; readonly patch: RuleConditionsPatch }
  | { readonly ok: false; readonly problems: readonly EditorProblem[] };

/** Thrown inside the parser for the first thing wrong; caught at the top and turned into a problem. */
class SnippetError extends Error {
  override readonly name = "SnippetError";
  readonly span: Span;

  constructor(span: Span, message: string) {
    super(message);
    this.span = span;
  }
}

function strings(node: JsonNode, key: string): { readonly value: string; readonly span: Span }[] {
  if (node.kind !== "array") {
    throw new SnippetError(node, `"${key}" is a list, such as ["accessgroup:Engineering"]`);
  }

  return node.items.map((item) => {
    if (item.kind !== "string") {
      throw new SnippetError(item, `Each entry of "${key}" is a string`);
    }

    return { value: item.value, span: item };
  });
}

function unique(ids: readonly string[]): string[] {
  return [...new Set(ids)];
}

/** A group by name, or by id when no group has that name. */
function findGroup(groups: readonly Group[], name: string): Group | undefined {
  return groups.find((group) => group.name === name) ?? groups.find((group) => group.id === name);
}

function groupIds(node: JsonNode, key: "src" | "dst", groups: readonly Group[]): string[] {
  return unique(
    strings(node, key).map(({ value, span }) => {
      if (value !== selfAlias && !value.startsWith(groupPrefix)) {
        throw new SnippetError(
          span,
          `Name a group as "${groupPrefix}<name>"; users, tags and addresses go in the policy file`,
        );
      }

      const group =
        value === selfAlias
          ? groups.find((candidate) => isSelf(candidate))
          : findGroup(groups, value.slice(groupPrefix.length));

      if (group === undefined) {
        throw new SnippetError(span, `No group is named "${value.slice(groupPrefix.length)}"`);
      }

      if (key === "src" && isSelf(group)) {
        throw new SnippetError(span, `${selfAlias} only works in "dst"`);
      }

      return group.id;
    }),
  );
}

function postureIds(node: JsonNode, postures: readonly Posture[]): string[] {
  return unique(
    strings(node, "srcPosture").map(({ value, span }) => {
      if (!value.startsWith(posturePrefix)) {
        throw new SnippetError(span, `Name a posture as "${posturePrefix}<name>"`);
      }

      const name = value.slice(posturePrefix.length);
      const posture =
        postures.find((candidate) => candidate.name === name) ??
        postures.find((candidate) => candidate.id === name);

      if (posture === undefined) {
        throw new SnippetError(span, `No posture is named "${name}"`);
      }

      return posture.id;
    }),
  );
}

const oneProtocol =
  'A rule allows one protocol: "*", "tcp:22,443", "udp:53" or "icmp:*". Write the rest in the policy file';

/** The rule protocol an "ip" entry's protocol stands for, or null when a rule cannot carry it. */
function entryProtocol(name: string, ports: string): Protocol | null {
  if (name === "*") {
    return "all";
  }

  if (name === "tcp" || name === "udp") {
    return name;
  }

  return (name === "icmp" || name === "ipv6-icmp") && ports === "*" ? "icmp" : null;
}

/** The protocol and ports of the grant's "ip" list, within what a rule can carry. */
function protocolOf(node: JsonNode): Pick<RuleDraft, "protocol" | "ports"> {
  const entries = strings(node, "ip");

  if (entries.length === 0) {
    throw new SnippetError(node, '"ip" lists what the rule allows, such as "*" or "tcp:443"');
  }

  let protocol: Protocol | null = null;
  let everyPort = false;
  const ports: string[] = [];

  for (const { value, span } of entries) {
    const issue = ipEntryIssue(value);

    if (issue !== null) {
      throw new SnippetError(span, issue);
    }

    const [name = "", list = ""] = value === "*" ? ["*", "*"] : value.split(":");
    const kind = entryProtocol(name, list);

    if (kind === null || (protocol !== null && (protocol !== kind || kind === "all"))) {
      throw new SnippetError(span, oneProtocol);
    }

    protocol = kind;

    if (list === "*") {
      everyPort = true;
    } else {
      ports.push(...list.split(","));
    }
  }

  return { protocol: protocol ?? "all", ports: everyPort ? "" : unique(ports).join(", ") };
}

function readObject(
  root: JsonObject,
  groups: readonly Group[],
  postures: readonly Posture[],
): RuleConditionsPatch {
  const seen = new Set<string>();
  const patch: { -readonly [Key in keyof RuleConditionsPatch]: RuleConditionsPatch[Key] } = {
    sources: [],
    destinations: [],
    postures: [],
    protocol: "all",
    ports: "",
  };

  for (const { key, value } of root.entries) {
    if (seen.has(key.name)) {
      throw new SnippetError(key, `"${key.name}" appears twice`);
    }

    seen.add(key.name);

    if (key.name === "src") {
      patch.sources = groupIds(value, "src", groups);
    } else if (key.name === "dst") {
      patch.destinations = groupIds(value, "dst", groups);
    } else if (key.name === "ip") {
      Object.assign(patch, protocolOf(value));
    } else if (key.name === "srcPosture") {
      patch.postures = postureIds(value, postures);
    } else {
      throw new SnippetError(
        key,
        `A rule has ${snippetKeys.map((name) => `"${name}"`).join(", ")}; "${key.name}" goes in the policy file`,
      );
    }
  }

  if (!seen.has("ip")) {
    throw new SnippetError(
      { from: root.from, to: root.from + 1 },
      'Add "ip": "*" for everything, or a list such as "tcp:22,443"',
    );
  }

  return patch;
}

/**
 * Reads a snippet back into the draft's conditions, or says what is wrong and where: syntax first,
 * as the policy file editor reports it, then the first entry the rule cannot carry.
 */
export function parseRuleSnippet(
  text: string,
  groups: readonly Group[],
  postures: readonly Posture[],
): SnippetResult {
  const { root, issues } = parseHujson(text);

  if (issues.length > 0) {
    return {
      ok: false,
      problems: issues.map(({ from, to, message }) => ({ from, to, severity: "error", message })),
    };
  }

  if (root.kind !== "object") {
    return {
      ok: false,
      problems: [
        {
          from: root.from,
          to: root.to,
          severity: "error",
          message: 'A rule is one grant: an object with "src", "dst" and "ip"',
        },
      ],
    };
  }

  try {
    return { ok: true, patch: readObject(root, groups, postures) };
  } catch (error) {
    if (error instanceof SnippetError) {
      return {
        ok: false,
        problems: [{ ...error.span, severity: "error", message: error.message }],
      };
    }

    throw error;
  }
}

/** "Line 3: message", for the line under the editor. */
export function problemLine(text: string, problem: EditorProblem): string {
  return `Line ${text.slice(0, problem.from).split("\n").length}: ${problem.message}`;
}

function same(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function bare(ports: string): string {
  return ports.replaceAll(/\s+/gv, "");
}

/**
 * The patch without the fields that did not change, so reading an untouched snippet back leaves the
 * draft as it was (ports compare without their spacing).
 */
export function changedConditions(
  draft: RuleDraft,
  patch: RuleConditionsPatch,
): Partial<RuleDraft> {
  return {
    ...(same(draft.sources, patch.sources) ? {} : { sources: patch.sources }),
    ...(same(draft.destinations, patch.destinations) ? {} : { destinations: patch.destinations }),
    ...(same(draft.postures, patch.postures) ? {} : { postures: patch.postures }),
    ...(draft.protocol === patch.protocol ? {} : { protocol: patch.protocol }),
    ...(bare(draft.ports) === bare(patch.ports) || !hasPorts(patch.protocol)
      ? {}
      : { ports: patch.ports }),
  };
}
