import { object, optional, pipe, transform, unknown } from "valibot";

import type { PanelControls } from "~/components/keys/panels.tsx";
import { statusFilters } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { optionalText } from "~/lib/search-text.ts";

export const kindFilters = ["all", "client", "federated"] as const;
export type KindFilter = (typeof kindFilters)[number];

function toStatus(value: unknown): StatusFilter | undefined {
  return statusFilters.find((known) => known === value);
}

function toKind(value: unknown): KindFilter | undefined {
  return kindFilters.find((known) => known === value);
}

const optionalStatus = optional(pipe(unknown(), transform(toStatus)));
const optionalKind = optional(pipe(unknown(), transform(toKind)));

/** The search of a keys page: its search box and the status/kind filters, absent when untouched. */
export const keysSearchSchema = object({
  q: optionalText,
  status: optionalStatus,
  kind: optionalKind,
});

export interface KeysSearch {
  readonly q?: string | undefined;
  readonly status?: StatusFilter | undefined;
  readonly kind?: KindFilter | undefined;
}

function searchFor(query: string, status: StatusFilter, kind: KindFilter): KeysSearch {
  return {
    q: query === "" ? undefined : query,
    status: status === "all" ? undefined : status,
    kind: kind === "all" ? undefined : kind,
  };
}

/**
 * The controls a key panel needs, over a route's search and its navigate. `go` replaces history
 * when `replace` is true (typing), else pushes (a filter pick).
 */
export function keyControls(
  search: KeysSearch,
  go: (next: KeysSearch, replace: boolean) => void,
): PanelControls {
  const query = search.q ?? "";
  const status = search.status ?? "all";
  const kind = search.kind ?? "all";
  return {
    query,
    status,
    kind,
    handleQueryChange: (value) => {
      go(searchFor(value, status, kind), true);
    },
    handleStatusChange: (value) => {
      go(searchFor(query, toStatus(value) ?? "all", kind), false);
    },
    handleKindChange: (value) => {
      go(searchFor(query, status, toKind(value) ?? "all"), false);
    },
    handleClear: () => {
      go(searchFor("", "all", "all"), true);
    },
  };
}
