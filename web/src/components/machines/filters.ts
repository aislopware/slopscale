import type { Node } from "~/api/queries.ts";
import { ownerId } from "~/components/machines/owner.ts";
import { nodeStatus } from "~/lib/node.ts";

/**
 * The segmented status filter above the machines table. There is no separate tab for an expired
 * key: such a machine is not reachable either, so it belongs with the offline ones.
 */
export const statusFilters = ["all", "online", "offline", "pending"] as const;

export type StatusFilter = (typeof statusFilters)[number];

export const defaultStatus: StatusFilter = "all";

export const statusFilterLabels: Record<StatusFilter, string> = {
  all: "All",
  online: "Connected",
  offline: "Offline",
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

  return status === "offline" ? current === "offline" || current === "expired" : current === status;
}

export interface MachineFilter {
  readonly status: StatusFilter;
  /** A user id; "" keeps every machine. Matches the owner and anyone it is shared with. */
  readonly user: string;
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
