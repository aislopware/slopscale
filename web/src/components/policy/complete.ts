import { snippetCompletion } from "@codemirror/autocomplete";
import type { Completion, CompletionContext, CompletionResult } from "@codemirror/autocomplete";
import { syntaxTree } from "@codemirror/language";
import { Facet } from "@codemirror/state";
import type { SyntaxNode } from "@lezer/common";

import { autogroupsFor, protocols } from "~/components/policy/aliases.ts";
import type { Side } from "~/components/policy/aliases.ts";
import { collectNames } from "~/components/policy/lint.ts";
import type { Names } from "~/components/policy/lint.ts";
import { autogroupDocs, sectionInfo, sections } from "~/components/policy/schema.ts";
import type { KeyInfo, Shape } from "~/components/policy/schema.ts";
import { parseHujson, placeAt } from "~/lib/hujson/ast.ts";
import type { JsonNode, PathStep, Place } from "~/lib/hujson/ast.ts";
import { knownAttributes } from "~/lib/posture/attributes.ts";

/** The users the tailnet has, as the policy names them, so a group can be filled from a list. */
export const policyUsers = Facet.define<readonly string[], readonly string[]>({
  combine: (values) => values.flat(),
});

const valueShapes: Readonly<Record<Shape, string>> = {
  object: "{}",
  array: "[]",
  string: '""',
  bool: "true",
  aliases: "[]",
  strings: "[]",
  postureRefs: "[]",
};

/** The keys an object at the path may hold: the sections at the top, a rule's keys inside one. */
function keysAt(path: readonly PathStep[]): readonly KeyInfo[] | null {
  const [section, second] = path;

  if (section === undefined) {
    return sections;
  }

  const info = typeof section === "string" ? sectionInfo(section) : undefined;

  if (info?.keys === undefined) {
    return null;
  }

  if (path.length === 1) {
    return info.shape === "object" ? info.keys : null;
  }

  return path.length === 2 && typeof second === "number" ? info.keys : null;
}

function keyCompletion(info: KeyInfo, hasColon: boolean): Completion {
  const base = { label: `"${info.name}"`, detail: info.shape, info: info.doc, type: "property" };

  if (hasColon) {
    return base;
  }

  const inside = valueShapes[info.shape];
  const template = `"${info.name}": ${inside.slice(0, 1)}\${}${inside.slice(1)}`;

  return snippetCompletion(template, base);
}

function completeKey(node: SyntaxNode, place: Place, taken: JsonNode): CompletionResult | null {
  const keys = keysAt(place.path);

  if (keys === null) {
    return null;
  }

  const present = new Set(
    taken.kind === "object" ? taken.entries.map((entry) => entry.key.name) : [],
  );
  const hasColon = node.nextSibling?.name === ":";
  const options = keys
    .filter((info) => !present.has(info.name) || info.name === place.key?.name)
    .map((info) => keyCompletion(info, hasColon));

  return { from: node.from, to: node.to, options, validFor: /^"[\w#]*"?$/v };
}

function named(values: readonly string[], type: string, detail?: string): Completion[] {
  return values.map((value) => ({
    label: value,
    type,
    ...(detail === undefined ? {} : { detail }),
    ...(autogroupDocs[value] === undefined ? {} : { info: autogroupDocs[value] }),
  }));
}

const machineSides = new Set<Side>(["src", "dst", "nodeAttrs"]);
const groupSides = new Set<Side>(["src", "dst", "sshSrc"]);

/** What may stand on a side of a rule: the names the file defines, the autogroups, the wildcard. */
function aliasesFor(side: Side, names: Names, users: readonly string[]): Completion[] {
  const machines = machineSides.has(side);

  return [
    ...named(users, "user", "user"),
    ...(groupSides.has(side) ? named([...names.groups], "group", "group") : []),
    ...named([...names.tags], "tag", "tag"),
    ...(machines ? named([...names.hosts], "host", "host") : []),
    ...named(autogroupsFor[side], "autogroup"),
    ...(machines ? named(["*"], "wildcard", "everyone") : []),
  ];
}

/** Owners and auto approvers: users, groups and tags, which is all the server takes there. */
function approvers(names: Names, users: readonly string[]): Completion[] {
  return [
    ...named(users, "user", "user"),
    ...named([...names.groups], "group", "group"),
    ...named([...names.tags], "tag", "tag"),
  ];
}

const loginNames = ["root", "autogroup:nonroot"];
const checkPeriods = ["always", "1h", "12h", "24h", "168h"];
const sshActions = ["accept", "check"];
const testProtocols = ["tcp", "udp", "sctp"];

interface Ask {
  readonly names: Names;
  readonly users: readonly string[];
}

/** The strings that fit at a key inside an ACL, grant, SSH rule or test. */
function ruleValues(section: string, key: string, ask: Ask): Completion[] {
  const { names, users } = ask;
  const ssh = section === "ssh" || section === "sshTests";
  const byKey: Readonly<Record<string, () => Completion[]>> = {
    src: () => aliasesFor(ssh ? "sshSrc" : "src", names, users),
    dst: () => aliasesFor(ssh ? "sshDst" : "dst", names, users),
    via: () => named([...names.tags], "tag", "tag"),
    target: () => aliasesFor("nodeAttrs", names, users),
    srcPosture: () => named([...names.postures], "posture"),
    action: () => named(ssh ? sshActions : ["accept"], "keyword"),
    proto: () => named(section === "tests" ? testProtocols : protocols, "keyword"),
    users: () => named(loginNames, "text"),
    accept: () => (ssh ? named(loginNames, "text") : aliasesFor("dst", names, users)),
    deny: () => (ssh ? named(loginNames, "text") : aliasesFor("dst", names, users)),
    check: () => named(loginNames, "text"),
    checkPeriod: () => named(checkPeriods, "text"),
    recorder: () => [
      ...named([...names.tags], "tag", "tag"),
      ...named([...names.hosts], "host", "host"),
    ],
  };

  return byKey[key]?.() ?? [];
}

/** The strings that fit where the caret is, by the path from the root down to the string. */
function stringValues(path: readonly PathStep[], ask: Ask): Completion[] {
  const [section, second, third] = path;
  const { names, users } = ask;

  if (section === "groups") {
    return named(users, "user", "user");
  }

  if (
    section === "tagOwners" ||
    (section === "autoApprovers" && (second === "exitNode" || second === "routes"))
  ) {
    return approvers(names, users);
  }

  if (section === "postures") {
    const attributes: Completion[] = knownAttributes.map((attribute) => ({
      label: attribute.name,
      detail: attribute.type,
      info: attribute.doc,
      type: "variable",
    }));

    return [...attributes, ...named(["custom:"], "variable")];
  }

  if (section === "defaultSrcPosture") {
    return named([...names.postures], "posture");
  }

  if (typeof section === "string" && typeof second === "number" && typeof third === "string") {
    return ruleValues(section, third, ask);
  }

  return [];
}

function completeString(node: SyntaxNode, place: Place, ask: Ask): CompletionResult | null {
  const options = stringValues(place.path, ask);

  if (options.length === 0) {
    return null;
  }

  const closed = node.to - node.from > 1;
  const to = closed ? node.to - 1 : node.to;

  return { from: node.from + 1, to, options, validFor: /^[\w:@.*\/\-]*$/v };
}

/** With nothing typed yet, an explicit request offers the keys or values that fit, quoted. */
function completeBare(context: CompletionContext, place: Place, ask: Ask): CompletionResult | null {
  if (!context.explicit) {
    return null;
  }

  if (place.node.kind === "object") {
    const keys = keysAt(place.path);
    const present = new Set(place.node.entries.map((entry) => entry.key.name));

    return keys === null
      ? null
      : {
          from: context.pos,
          options: keys
            .filter((info) => !present.has(info.name))
            .map((info) => keyCompletion(info, false)),
        };
  }

  if (place.node.kind === "array") {
    const options = stringValues([...place.path, place.node.items.length], ask).map(
      (option): Completion => Object.assign(option, { apply: `"${option.label}"` }),
    );

    return options.length === 0 ? null : { from: context.pos, options };
  }

  return null;
}

const skipped = new Set(["LineComment", "BlockComment"]);

/**
 * Completions for the policy: section names at the top, a rule's keys inside one, and for a string
 * the users, groups, tags, hosts and autogroups that may stand there, from what the file defines.
 */
export function completePolicy(context: CompletionContext): CompletionResult | null {
  const node = syntaxTree(context.state).resolveInner(context.pos, -1);

  if (skipped.has(node.name)) {
    return null;
  }

  const parsed = parseHujson(context.state.doc.toString());
  const ask: Ask = { names: collectNames(parsed.root), users: context.state.facet(policyUsers) };

  if (node.name === "PropertyName") {
    const place = placeAt(parsed.root, node.from);

    return completeKey(node, place, place.node);
  }

  const place = placeAt(parsed.root, context.pos);

  if (node.name === "String") {
    return completeString(node, place, ask);
  }

  return completeBare(context, place, ask);
}
