import { isPrefix, isServiceName } from "~/components/policy/aliases.ts";
import type { AliasKind } from "~/components/policy/aliases.ts";
import { Lint, anyAlias } from "~/components/policy/lint-context.ts";
import type { Names, Problem } from "~/components/policy/lint-context.ts";
import { lintAcl, lintGrant, lintSsh } from "~/components/policy/lint-rules.ts";
import {
  autoApproverKeys,
  nodeAttrKeys,
  sections,
  sshTestKeys,
  testKeys,
} from "~/components/policy/schema.ts";
import { parseHujson } from "~/lib/hujson/ast.ts";
import type { JsonNode, JsonString, Parsed } from "~/lib/hujson/ast.ts";
import { parseExpression } from "~/lib/posture/expression.ts";

export type { Names, Problem } from "~/components/policy/lint-context.ts";

export interface LintResult {
  readonly parsed: Parsed;
  readonly names: Names;
  readonly problems: readonly Problem[];
}

const approvers: readonly AliasKind[] = ["user", "group", "tag"];

function objectKeys(node: JsonNode | undefined): Set<string> {
  return new Set(node?.kind === "object" ? node.entries.map((entry) => entry.key.name) : []);
}

/** The groups, tags, hosts and postures the file defines, by their keys. */
export function collectNames(root: JsonNode): Names {
  const section = (name: string): JsonNode | undefined =>
    root.kind === "object"
      ? root.entries.find((entry) => entry.key.name === name)?.value
      : undefined;

  return {
    groups: objectKeys(section("groups")),
    tags: objectKeys(section("tagOwners")),
    hosts: objectKeys(section("hosts")),
    postures: objectKeys(section("postures")),
  };
}

function lintGroups(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "groups");

  for (const { key, value } of object?.entries ?? []) {
    if (!key.name.startsWith("group:") || key.name === "group:") {
      lint.error(key, 'A group name starts with "group:"');
    }

    for (const member of lint.strings(value, `group ${key.name}`)) {
      if (member.value.startsWith("group:")) {
        lint.error(member, "A group cannot contain another group");
      } else if (!member.value.includes("@")) {
        lint.error(member, `A user name has an @ in it, such as "${member.value}@"`);
      }
    }
  }
}

function lintHosts(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "hosts");

  for (const { key, value } of object?.entries ?? []) {
    if (key.name === "" || key.name.includes(":") || key.name.includes("@")) {
      lint.error(key, "A host name has no colon or @ in it");
    }

    if (value.kind === "string" && !isPrefix(value.value)) {
      lint.error(value, `"${value.value}" is not an address or a range such as 10.0.0.0/24`);
    } else if (value.kind !== "string" && value.kind !== "error") {
      lint.error(value, "A host is an address or a range, as a string");
    }
  }
}

function lintTagOwners(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "tagOwners");

  for (const { key, value } of object?.entries ?? []) {
    if (!/^tag:[a-zA-Z]/v.test(key.name)) {
      lint.error(key, 'A tag name starts with "tag:" and then a letter');
    }

    lint.aliases(value, { side: "src", allowed: approvers, what: `owners of ${key.name}` });
  }
}

/** Reports a bad expression at the spot inside its string when the source has no escapes. */
function lintExpression(lint: Lint, node: JsonString): void {
  const result = parseExpression(node.value);

  if (result.ok) {
    return;
  }

  const plain = node.to - node.from === node.value.length + 2;
  const span = plain
    ? { from: node.from + 1 + result.error.from, to: node.from + 1 + result.error.to }
    : node;

  lint.error(span, result.error.message);
}

function lintPostures(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "postures");

  for (const { key, value } of object?.entries ?? []) {
    if (!key.name.startsWith("posture:") || key.name === "posture:") {
      lint.error(key, 'A posture name starts with "posture:"');
    } else if (key.name.startsWith("posture:#")) {
      lint.error(key, '"posture:#" names are reserved for postures from the Postures page');
    }

    const expressions = lint.strings(value, `posture ${key.name}`);

    if (value.kind === "array" && expressions.length === 0) {
      lint.error(value, "A posture needs at least one expression");
    }

    for (const expression of expressions) {
      lintExpression(lint, expression);
    }
  }
}

function lintAutoApprovers(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "autoApprovers");

  if (object === null) {
    return;
  }

  lint.keys(object, autoApproverKeys, "autoApprovers");

  for (const { key, value } of object.entries) {
    if (key.name === "exitNode") {
      lint.aliases(value, { side: "src", allowed: approvers, what: "exitNode" });
    } else if (key.name === "routes") {
      const routes = lint.object(value, "routes");

      for (const route of routes?.entries ?? []) {
        if (!isPrefix(route.key.name)) {
          lint.error(route.key, `"${route.key.name}" is not a range such as 10.0.0.0/24`);
        }

        lint.aliases(route.value, {
          side: "src",
          allowed: approvers,
          what: `approvers of ${route.key.name}`,
        });
      }
    } else if (key.name === "services") {
      const services = lint.object(value, "services");

      for (const service of services?.entries ?? []) {
        if (!isServiceName(service.key.name)) {
          lint.error(service.key, `"${service.key.name}" is not a service name such as "svc:web"`);
        }

        lint.aliases(service.value, {
          side: "src",
          allowed: approvers,
          what: `hosts of ${service.key.name}`,
        });
      }
    }
  }
}

function lintIpPool(lint: Lint, value: JsonNode): void {
  for (const pool of lint.strings(value, "ipPool")) {
    if (!isPrefix(pool.value) || !pool.value.includes("/")) {
      lint.error(pool, `"${pool.value}" is not a range such as 100.64.0.0/10`);
    }
  }
}

function lintNodeAttr(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "a nodeAttrs entry");

  if (object === null) {
    return;
  }

  lint.keys(object, nodeAttrKeys, "a nodeAttrs entry");

  for (const { key, value } of object.entries) {
    if (key.name === "target") {
      lint.aliases(value, { side: "nodeAttrs", allowed: anyAlias, what: "target" });
    } else if (key.name === "attr") {
      lint.strings(value, "attr");
    } else if (key.name === "app") {
      lint.object(value, "app");
    } else if (key.name === "ipPool") {
      lintIpPool(lint, value);
    }
  }
}

const testProtocols = new Set(["", "tcp", "udp", "sctp"]);

function lintTest(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "a test");

  if (object === null) {
    return;
  }

  lint.keys(object, testKeys, "a test");

  const names = new Set(object.entries.map((entry) => entry.key.name));

  if (!names.has("accept") && !names.has("deny")) {
    lint.error(object, 'A test needs "accept" or "deny"');
  }

  for (const { key, value } of object.entries) {
    if (key.name === "src" && value.kind === "string") {
      lint.alias(value, { side: "src", allowed: anyAlias });
    } else if (key.name === "proto" && value.kind === "string" && !testProtocols.has(value.value)) {
      lint.error(value, "A test protocol is tcp, udp or sctp");
    } else if (key.name === "accept" || key.name === "deny") {
      for (const target of lint.strings(value, key.name)) {
        if (!/:\d+$/v.test(target.value)) {
          lint.error(target, 'A test destination ends in one port, such as "tag:web:443"');
        }
      }
    }
  }
}

const sshTestSources: readonly AliasKind[] = ["user", "group", "tag", "autogroup"];
const sshTestDestinations: readonly AliasKind[] = ["user", "tag", "autogroup"];

function lintSshTest(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "an SSH test");

  if (object === null) {
    return;
  }

  lint.keys(object, sshTestKeys, "an SSH test");

  for (const { key, value } of object.entries) {
    if (key.name === "src" && value.kind === "string") {
      lint.alias(value, { side: "sshSrc", allowed: sshTestSources });
    } else if (key.name === "dst") {
      lint.aliases(value, { side: "sshDst", allowed: sshTestDestinations, what: "dst" });
    } else if (key.name === "accept" || key.name === "deny" || key.name === "check") {
      lint.strings(value, key.name);
    }
  }
}

type SectionLint = (lint: Lint, value: JsonNode) => void;

/** A section that is a list of entries, each checked on its own. */
function eachOf(name: string, entry: SectionLint): SectionLint {
  return (lint, value) => {
    for (const item of lint.array(value, name)?.items ?? []) {
      entry(lint, item);
    }
  };
}

const sectionLints: Readonly<Record<string, SectionLint>> = {
  groups: lintGroups,
  hosts: lintHosts,
  tagOwners: lintTagOwners,
  acls: eachOf("acls", lintAcl),
  grants: eachOf("grants", lintGrant),
  ssh: eachOf("ssh", lintSsh),
  nodeAttrs: eachOf("nodeAttrs", lintNodeAttr),
  autoApprovers: lintAutoApprovers,
  tests: eachOf("tests", lintTest),
  sshTests: eachOf("sshTests", lintSshTest),
  postures: lintPostures,
  defaultSrcPosture: (lint, value) => {
    lint.postureRefs(value, "defaultSrcPosture");
  },
  randomizeClientPort: (lint, value) => {
    lint.bool(value, "randomizeClientPort");
  },
};

/**
 * Checks a policy draft the way the server will, as far as the text alone allows: syntax, the shape
 * of every section, the form of every alias and every reference to a group, tag, host or posture
 * the file defines. What needs the server (users that exist, tags in use) is left to it.
 */
export function lintPolicy(text: string): LintResult {
  const parsed = parseHujson(text);
  const lint = new Lint(collectNames(parsed.root));
  const blank = text.trim() === "";

  // A blank draft is a policy that says nothing, which the server accepts; it is not a syntax error.
  for (const issue of blank ? [] : parsed.issues) {
    lint.error(issue, issue.message);
  }

  const root = blank ? null : lint.object(parsed.root, "The policy");

  if (root !== null) {
    lint.keys(root, sections, "the policy");

    for (const { key, value } of root.entries) {
      sectionLints[key.name]?.(lint, value);
    }
  }

  return { parsed, names: lint.names, problems: lint.problems };
}
