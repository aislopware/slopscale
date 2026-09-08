import { object, optional, pipe, transform, unknown } from "valibot";

import type { PanelControls } from "~/components/keys/panels.tsx";
import { statusFilters } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { optionalText } from "~/lib/search-text.ts";

function toStatus(value: unknown): StatusFilter | undefined {
  return statusFilters.find((known) => known === value);
}

const optionalStatus = optional(pipe(unknown(), transform(toStatus)));

/** The search of a keys page: its search box and the status filter, both absent when untouched. */
export const keysSearchSchema = object({ q: optionalText, status: optionalStatus });

export interface KeysSearch {
  readonly q?: string | undefined;
  readonly status?: StatusFilter | undefined;
}

function searchFor(query: string, status: StatusFilter): KeysSearch {
  return { q: query === "" ? undefined : query, status: status === "all" ? undefined : status };
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
  return {
    query,
    status,
    handleQueryChange: (value) => {
      go(searchFor(value, status), true);
    },
    handleStatusChange: (value) => {
      go(searchFor(query, toStatus(value) ?? "all"), false);
    },
    handleClear: () => {
      go(searchFor("", "all"), true);
    },
  };
}
