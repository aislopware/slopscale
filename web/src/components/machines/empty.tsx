import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import type { ReactElement } from "react";

import type { StatusFilter } from "~/components/machines/filters.ts";
import { tableEmptyClass } from "~/components/table/empty.ts";

export interface MachinesEmptyProps {
  /** How many machines exist at all, before any filter. */
  readonly total: number;
  readonly status: StatusFilter;
  /** Whether a search or a user filter is narrowing the rows as well. */
  readonly narrowed: boolean;
  /** The caller sees only their own machines, so an empty list means they have none yet. */
  readonly ownOnly?: boolean;
  readonly onClearFilters: () => void;
}

/**
 * What the table says when it has no rows: how machines get here, or the way out of a filter that
 * hides them all. It does not repeat the toolbar's "Add machine" an inch above it.
 */
export function MachinesEmpty({
  total,
  status,
  narrowed,
  ownOnly = false,
  onClearFilters,
}: MachinesEmptyProps): ReactElement {
  if (total === 0 && ownOnly) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines of yours yet"
        description="Sign in on a machine and it appears here. Machines other users share with you show here too."
      />
    );
  }

  if (total === 0) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines yet"
        description="Register a machine with a pre-auth key or by signing in. New machines appear here on their own."
      />
    );
  }

  if (status === "pending" && !narrowed) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines need approval"
        contents={
          <Button variant="secondary" onClick={onClearFilters}>
            View all machines
          </Button>
        }
      />
    );
  }

  return (
    <Empty
      size="sm"
      className={tableEmptyClass}
      title="No machines match"
      description="Try a different search or filter."
      contents={
        <Button variant="secondary" onClick={onClearFilters}>
          Clear filters
        </Button>
      }
    />
  );
}
