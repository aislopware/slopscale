import type { AccessRule } from "~/api/queries.ts";
import { hasPorts, portsError, toProtocol } from "~/components/access/model.ts";
import type { Protocol } from "~/components/access/model.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { fromLocalInput, toLocalInput } from "~/lib/time.ts";

type RuleBody = Parameters<AccessMutations["createRule"]["mutate"]>[0]["body"];

/** A rule as the editor holds it while the operator changes it. */
export interface RuleDraft {
  readonly name: string;
  readonly description: string;
  readonly sources: readonly string[];
  readonly destinations: readonly string[];
  readonly postures: readonly string[];
  readonly protocol: Protocol;
  readonly ports: string;
  readonly bidirectional: boolean;
  readonly enabled: boolean;
  /** Whether the rule stops applying at a set time. */
  readonly expiring: boolean;
  /** A datetime-local value; empty until the operator picks one. */
  readonly expires: string;
}

export function ruleDraftFrom(rule: AccessRule | undefined): RuleDraft {
  const expires = toLocalInput(rule?.expiresAt);

  return {
    name: rule?.name ?? "",
    description: rule?.description ?? "",
    sources: rule?.sourceGroupIds ?? [],
    destinations: rule?.destinationGroupIds ?? [],
    postures: rule?.postureIds ?? [],
    protocol: toProtocol(rule?.protocol ?? "all"),
    ports: rule?.ports ?? "",
    bidirectional: rule?.bidirectional ?? false,
    enabled: rule?.enabled ?? true,
    expiring: expires !== "",
    expires,
  };
}

/** What is wrong with each field of the draft, written for the line under it. */
export interface RuleDraftErrors {
  readonly name?: string;
  readonly sources?: string;
  readonly destinations?: string;
  readonly ports?: string;
  readonly expires?: string;
}

export function ruleDraftErrors(draft: RuleDraft): RuleDraftErrors {
  const ports = hasPorts(draft.protocol) ? portsError(draft.ports) : null;

  return {
    ...(draft.name.trim() === "" ? { name: "Name is required." } : {}),
    ...(draft.sources.length === 0 ? { sources: "Pick at least one source group." } : {}),
    ...(draft.destinations.length === 0
      ? { destinations: "Pick at least one destination group." }
      : {}),
    ...(ports === null ? {} : { ports }),
    ...(draft.expiring && draft.expires === ""
      ? { expires: "Pick when the rule stops applying." }
      : {}),
  };
}

/**
 * The request body for the draft. A rule to Own machines cannot go both ways, so the flag is
 * dropped there whatever the switch says.
 */
export function ruleBody(draft: RuleDraft, toOwnMachines: boolean): RuleBody {
  const expiresAt = draft.expiring ? fromLocalInput(draft.expires) : undefined;

  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    enabled: draft.enabled,
    protocol: draft.protocol,
    ports: hasPorts(draft.protocol) ? draft.ports.trim() : "",
    bidirectional: draft.bidirectional && !toOwnMachines,
    sourceGroupIds: [...draft.sources],
    destinationGroupIds: [...draft.destinations],
    postureIds: [...draft.postures],
    ...(expiresAt === undefined ? {} : { expiresAt }),
  };
}
