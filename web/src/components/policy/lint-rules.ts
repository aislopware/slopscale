import {
  ipEntryIssue,
  isCheckPeriod,
  isDefaultRoute,
  portsIssue,
  protocolIssue,
  roleAutogroups,
  splitPorts,
} from "~/components/policy/aliases.ts";
import type { AliasKind } from "~/components/policy/aliases.ts";
import { anyAlias } from "~/components/policy/lint-context.ts";
import type { Lint } from "~/components/policy/lint-context.ts";
import { aclKeys, grantKeys, sshKeys } from "~/components/policy/schema.ts";
import type { JsonNode, JsonString } from "~/lib/hujson/ast.ts";

/** The sources autogroup:self accepts: the ones that stand for users. */
const userSources = new Set<string>(["autogroup:member", ...roleAutogroups]);
const userKinds = new Set<AliasKind | null>(["user", "group"]);

function isUserSource(source: JsonString, kind: AliasKind | null, wildcardOk: boolean): boolean {
  if (userKinds.has(kind)) {
    return true;
  }

  if (kind === "autogroup") {
    return userSources.has(source.value);
  }

  return kind === "wildcard" && wildcardOk;
}

interface Rule {
  readonly sources: readonly JsonString[];
  readonly destinations: readonly JsonString[];
}

/**
 * Autogroup:self as a destination only makes sense from sources that stand for users, since it
 * resolves to the same user's machines; a tag or an address has no user to be the same as.
 */
function lintSelf(lint: Lint, rule: Rule, wildcardOk: boolean): void {
  if (!rule.destinations.some((destination) => destination.value === "autogroup:self")) {
    return;
  }

  for (const source of rule.sources) {
    const kind = lint.alias(source, { side: "src", allowed: anyAlias });

    if (source.value === "autogroup:shared") {
      lint.error(source, "autogroup:shared cannot be used with an autogroup:self destination");
    } else if (!isUserSource(source, kind, wildcardOk)) {
      lint.error(
        source,
        "With autogroup:self as the destination, a source must be a user, a group or autogroup:member",
      );
    }
  }
}

function aclDestination(lint: Lint, item: JsonString): JsonString | null {
  const split = splitPorts(item.value);

  if (split === null) {
    lint.error(item, 'An ACL destination ends in its ports, such as "tag:web:443" or "*:*"');

    return null;
  }

  const alias: JsonString = {
    kind: "string",
    value: split.alias,
    from: item.from,
    to: item.from + 1 + split.alias.length,
  };
  const ports = portsIssue(split.ports);

  if (ports !== null) {
    lint.error({ from: alias.to + 1, to: item.to - 1 }, ports);
  }

  lint.alias(alias, { side: "dst", allowed: anyAlias });

  return alias;
}

function aclDestinations(lint: Lint, node: JsonNode): JsonString[] {
  return lint
    .strings(node, "dst")
    .map((item) => aclDestination(lint, item))
    .filter((alias) => alias !== null);
}

function lintProtocol(lint: Lint, value: JsonNode): void {
  if (value.kind !== "string") {
    if (value.kind !== "error") {
      lint.error(value, "proto is a protocol name or number, as a string");
    }

    return;
  }

  const issue = protocolIssue(value.value);

  if (issue !== null) {
    lint.error(value, issue);
  }
}

export function lintAcl(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "an ACL");

  if (object === null) {
    return;
  }

  lint.keys(object, aclKeys, "an ACL");

  let sources: readonly JsonString[] = [];
  let destinations: readonly JsonString[] = [];

  for (const { key, value } of object.entries) {
    if (key.name === "action") {
      if (value.kind === "string" && value.value !== "accept") {
        lint.error(value, 'An ACL action is "accept"');
      }
    } else if (key.name === "proto") {
      lintProtocol(lint, value);
    } else if (key.name === "src") {
      sources = lint.aliases(value, { side: "src", allowed: anyAlias, what: "src" });
    } else if (key.name === "dst") {
      destinations = aclDestinations(lint, value);
    } else if (key.name === "srcPosture") {
      lint.postureRefs(value, "srcPosture");
    }
  }

  lintSelf(lint, { sources, destinations }, true);
}

function lintApp(lint: Lint, node: JsonNode): boolean {
  const object = lint.object(node, "app");

  for (const { key, value } of object?.entries ?? []) {
    if (!key.name.includes("/") || key.name.includes("://")) {
      lint.error(key, 'A capability is named domain/path, such as "example.com/cap"');
    } else if (key.name.startsWith("tailscale.com/")) {
      lint.error(key, "Capabilities under tailscale.com are reserved");
    }

    lint.array(value, `the values of ${key.name}`);
  }

  return object !== null && object.entries.length > 0;
}

function lintIp(lint: Lint, node: JsonNode): boolean {
  const entries = lint.strings(node, "ip");

  for (const entry of entries) {
    const issue = ipEntryIssue(entry.value);

    if (issue !== null) {
      lint.error(entry, issue);
    }
  }

  return entries.length > 0;
}

function lintGrantDestinations(lint: Lint, node: JsonNode): JsonString[] {
  const destinations = lint.aliases(node, { side: "dst", allowed: anyAlias, what: "dst" });

  for (const destination of destinations) {
    if (isDefaultRoute(destination.value)) {
      lint.error(destination, 'Write "*" or autogroup:internet rather than a default route');
    }
  }

  return destinations;
}

export function lintGrant(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "a grant");

  if (object === null) {
    return;
  }

  lint.keys(object, grantKeys, "a grant");

  let sources: readonly JsonString[] = [];
  let destinations: readonly JsonString[] = [];
  let access = false;
  let via = false;

  for (const { key, value } of object.entries) {
    if (key.name === "src") {
      sources = lint.aliases(value, { side: "src", allowed: anyAlias, what: "src" });
    } else if (key.name === "dst") {
      destinations = lintGrantDestinations(lint, value);
    } else if (key.name === "ip") {
      access = lintIp(lint, value) || access;
    } else if (key.name === "app") {
      access = lintApp(lint, value) || access;
    } else if (key.name === "via") {
      via = lint.aliases(value, { side: "src", allowed: ["tag"], what: "via" }).length > 0;
    } else if (key.name === "srcPosture") {
      lint.postureRefs(value, "srcPosture");
    }
  }

  if (!access) {
    lint.error(object, 'A grant needs "ip" or "app" to say what it grants');
  }

  if (via && sources.some((source) => source.value === "autogroup:shared")) {
    lint.error(object, "autogroup:shared cannot be used in a via grant");
  }

  lintSelf(lint, { sources, destinations }, false);
}

const sshSources: readonly AliasKind[] = ["user", "group", "tag", "autogroup"];
const sshDestinations: readonly AliasKind[] = ["user", "tag", "autogroup"];

function lintSshDestinations(lint: Lint, node: JsonNode): JsonString[] {
  const strings = lint.strings(node, "dst");

  for (const item of strings) {
    if (item.value === "*") {
      lint.error(
        item,
        "An SSH destination cannot be *; use autogroup:member, autogroup:tagged or tags",
      );
    } else if (lint.names.hosts.has(item.value)) {
      lint.error(item, "An SSH destination cannot be a host from the hosts section");
    } else {
      lint.alias(item, { side: "sshDst", allowed: sshDestinations });
    }
  }

  return strings;
}

function lintSshUsers(lint: Lint, node: JsonNode): void {
  const users = lint.strings(node, "users");

  if (node.kind === "array" && users.length === 0) {
    lint.error(node, "An SSH rule needs at least one login user");
  }

  for (const user of users) {
    if (user.value === "" || user.value === "*") {
      lint.error(user, 'A login is a user name such as "root" or autogroup:nonroot, not "*"');
    }
  }
}

function lintSshCombination(lint: Lint, rule: Rule): void {
  const tagSource = rule.sources.some(
    (source) => source.value.startsWith("tag:") || source.value === "autogroup:tagged",
  );

  for (const destination of rule.destinations) {
    if (tagSource && destination.value.includes("@")) {
      lint.error(
        destination,
        "A tagged source cannot SSH to a user's machines; use autogroup:tagged or tags as destinations",
      );
    }
  }

  lintSelf(lint, rule, false);
}

function lintCheckPeriod(lint: Lint, value: JsonNode, checks: boolean): void {
  if (!checks) {
    lint.error(value, 'checkPeriod only applies with action "check"');
  } else if (value.kind === "string" && !isCheckPeriod(value.value)) {
    lint.error(value, 'checkPeriod is a duration such as "12h" or "always"');
  }
}

function lintAcceptEnv(lint: Lint, value: JsonNode): void {
  for (const env of lint.strings(value, "acceptEnv")) {
    if (env.value === "") {
      lint.error(env, "An acceptEnv entry cannot be empty");
    }
  }
}

const sshRecorder = { side: "dst", allowed: ["tag", "host", "prefix"], what: "recorder" } as const;

export function lintSsh(lint: Lint, node: JsonNode): void {
  const object = lint.object(node, "an SSH rule");

  if (object === null) {
    return;
  }

  lint.keys(object, sshKeys, "an SSH rule");

  const names = new Set(object.entries.map((entry) => entry.key.name));

  for (const required of ["action", "src", "dst", "users"]) {
    if (!names.has(required)) {
      lint.error(object, `An SSH rule needs "${required}"`);
    }
  }

  const action = object.entries.find((entry) => entry.key.name === "action")?.value;
  const checks = action?.kind === "string" && action.value === "check";
  let sources: readonly JsonString[] = [];
  let destinations: readonly JsonString[] = [];

  for (const { key, value } of object.entries) {
    if (key.name === "action") {
      if (value.kind === "string" && value.value !== "accept" && value.value !== "check") {
        lint.error(value, 'An SSH action is "accept" or "check"');
      }
    } else if (key.name === "src") {
      sources = lint.aliases(value, { side: "sshSrc", allowed: sshSources, what: "src" });
    } else if (key.name === "dst") {
      destinations = lintSshDestinations(lint, value);
    } else if (key.name === "users") {
      lintSshUsers(lint, value);
    } else if (key.name === "checkPeriod") {
      lintCheckPeriod(lint, value, checks);
    } else if (key.name === "acceptEnv") {
      lintAcceptEnv(lint, value);
    } else if (key.name === "recorder") {
      lint.aliases(value, sshRecorder);
    } else if (key.name === "enforceRecorder") {
      lint.bool(value, "enforceRecorder");
    }
  }

  lintSshCombination(lint, { sources, destinations });
}
