import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { DevicesIcon, PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { StatusFilter } from "~/components/machines/filters.ts";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";

export interface MachinesEmptyProps {
  /** How many machines exist at all, before any filter. */
  readonly total: number;
  readonly status: StatusFilter;
  /** Whether a search or a user filter is narrowing the rows as well. */
  readonly narrowed: boolean;
  readonly canCreateKeys: boolean;
  readonly onAddMachine: () => void;
  readonly onClearFilters: () => void;
}

/**
 * What the table says when it has no rows. Each case offers the one action that fixes it: an empty
 * tailnet gets the machine it is missing, a filter that hides everything gets a way out.
 */
export function MachinesEmpty({
  total,
  status,
  narrowed,
  canCreateKeys,
  onAddMachine,
  onClearFilters,
}: MachinesEmptyProps): ReactElement {
  if (total === 0) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        icon={<DevicesIcon size={emptyIconSize} />}
        title="No machines yet"
        description="Register a device with a pre-auth key or by signing in. It appears here immediately."
        contents={
          canCreateKeys ? (
            <Button variant="secondary" icon={PlusIcon} onClick={onAddMachine}>
              Add machine
            </Button>
          ) : null
        }
      />
    );
  }

  if (status === "pending" && !narrowed) {
    return (
      <Empty
        size="sm"
        className={tableEmptyClass}
        title="No machines need approval"
        description="Every machine on the tailnet has been approved."
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
