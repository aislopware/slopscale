import type { Node } from "~/api/queries.ts";
import { ownerId } from "~/components/machines/owner.ts";
import { nodeStatus } from "~/lib/node.ts";

/**
 * The segmented status filter above the machines table. There is no separate tab for an expired key
 * or a suspended machine: neither is reachable, so both belong with the offline ones.
 */
export const statusFilters = ["all", "online", "offline", "pending"] as const;

export type StatusFilter = (typeof statusFilters)[number];

export const defaultStatus: StatusFilter = "all";

export const statusFilterLabels: Record<StatusFilter, string> = {
  all: "All",
  online: "Connected",
  offline: "Disconnected",
  pending: "Needs approval",
};

/** Reads a status out of the URL, falling back to the default for anything unknown. */
export function toStatusFilter(value: string | undefined): StatusFilter {
  return statusFilters.find((filter) => filter === value) ?? defaultStatus;
}

function matchesStatus(node: Node, status: StatusFilter, now: Date): boolean {
  if (status === "all") {
    return true;
  }

  const current = nodeStatus(node, now);

  return status === "offline" ? current !== "online" && current !== "pending" : current === status;
}

/**
 * Whether the machine's hardware attestation key currently proves it. "Not attested" covers both a
 * machine that never signed a map request with one and a machine that no longer does, because
 * either way a posture checking `node:hardwareAttested` fails.
 */
export const attestationFilters = ["any", "attested", "unattested"] as const;

export type AttestationFilter = (typeof attestationFilters)[number];

export const defaultAttestation: AttestationFilter = "any";

export const attestationFilterLabels: Record<AttestationFilter, string> = {
  any: "Any attestation",
  attested: "Attested",
  unattested: "Not attested",
};

/** Reads an attestation filter out of the URL, falling back to the default for anything unknown. */
export function toAttestationFilter(value: string | undefined): AttestationFilter {
  return attestationFilters.find((filter) => filter === value) ?? defaultAttestation;
}

/** Whether any machine has an attestation record, which is what makes the filter worth offering. */
export function anyAttestation(nodes: readonly Node[]): boolean {
  return nodes.some((node) => node.hardwareAttestation !== undefined);
}

function matchesAttestation(node: Node, attestation: AttestationFilter): boolean {
  if (attestation === "any") {
    return true;
  }

  const attested = node.hardwareAttestation?.attested ?? false;

  return attestation === "attested" ? attested : !attested;
}

export interface MachineFilter {
  readonly status: StatusFilter;
  /** A user id; "" keeps every machine. Matches the owner and anyone it is shared with. */
  readonly user: string;
  /** One ACL tag, as the machine carries it (`tag:prod`); "" keeps every machine. */
  readonly tag: string;
  readonly attestation: AttestationFilter;
}

/** Every tag any machine carries, in the order the tag filter offers them. */
export function tagOptions(nodes: readonly Node[]): string[] {
  return [...new Set(nodes.flatMap((node) => node.tags))].toSorted((left, right) =>
    left.localeCompare(right),
  );
}

export function filterNodes(
  nodes: readonly Node[],
  filter: MachineFilter,
  now: Date = new Date(),
): Node[] {
  return nodes.filter((node) => {
    if (
      filter.user !== "" &&
      ownerId(node) !== filter.user &&
      !node.sharedWith.includes(filter.user)
    ) {
      return false;
    }

    if (filter.tag !== "" && !node.tags.includes(filter.tag)) {
      return false;
    }

    if (!matchesAttestation(node, filter.attestation)) {
      return false;
    }

    return matchesStatus(node, filter.status, now);
  });
}

const msPerDay = 86_400_000;
/** An expiry further out than this week is not worth a line in a dense table. */
const expiryNoticeDays = 7;

/** Whether the machine's key has expired or is about to, which is worth saying in a list. */
export function expiryWorthShowing(expiry: Date | null, now: Date = new Date()): boolean {
  return expiry !== null && expiry.getTime() - now.getTime() < expiryNoticeDays * msPerDay;
}

/** How many machines each tab would show, for the counts beside the tab labels. */
export function statusCounts(
  nodes: readonly Node[],
  now: Date = new Date(),
): Record<StatusFilter, number> {
  const counts: Record<StatusFilter, number> = { all: 0, online: 0, offline: 0, pending: 0 };

  for (const status of statusFilters) {
    counts[status] = nodes.filter((node) => matchesStatus(node, status, now)).length;
  }

  return counts;
}

/** A whole filter state, as the toolbar and the chips read it. */
export interface MachineFilterState extends MachineFilter {
  /** The free-text search; "" for none. */
  readonly query: string;
}

/** The filters as a URL carries them; every one is optional and any of them may be nonsense. */
export interface RawMachineSearch {
  readonly q?: string | undefined;
  readonly status?: string | undefined;
  readonly user?: string | undefined;
  readonly tag?: string | undefined;
  readonly attested?: string | undefined;
}

/** The same parameters narrowed, which is what the page writes back. */
export interface MachineSearch extends RawMachineSearch {
  readonly status?: StatusFilter | undefined;
  readonly attested?: AttestationFilter | undefined;
}

/** What the URL asks for. An absent or unknown parameter reads as that filter's default. */
export function filtersFromSearch(search: RawMachineSearch): MachineFilterState {
  return {
    query: search.q ?? "",
    status: toStatusFilter(search.status),
    user: search.user ?? "",
    tag: search.tag ?? "",
    attestation: toAttestationFilter(search.attested),
  };
}

/**
 * The URL for a filter state. A filter left at its default is absent, so the plain `/machines` the
 * sidebar links to is exactly the URL the unfiltered page produces.
 */
export function searchFromFilters(state: MachineFilterState): MachineSearch {
  return {
    ...(state.query === "" ? {} : { q: state.query }),
    ...(state.status === defaultStatus ? {} : { status: state.status }),
    ...(state.user === "" ? {} : { user: state.user }),
    ...(state.tag === "" ? {} : { tag: state.tag }),
    ...(state.attestation === defaultAttestation ? {} : { attested: state.attestation }),
  };
}
