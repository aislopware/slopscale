import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import {
  statusFilterLabels,
  statusFilters,
  toStatusFilter,
} from "~/components/machines/filters.ts";
import type { StatusFilter } from "~/components/machines/filters.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { userLabel } from "~/lib/node.ts";

export interface MachinesToolbarProps {
  readonly me: Me;
  readonly query: string;
  readonly status: StatusFilter;
  /** The selected user id; "" for every user. */
  readonly user: string;
  /** Undefined while the caller may not read users, which hides the filter. */
  readonly users: readonly User[] | undefined;
  readonly counts: Record<StatusFilter, number>;
  /** Opens the page's "Add machine" dialog, which the empty state shares. */
  readonly onAddMachine: () => void;
  readonly onQueryChange: (value: string) => void;
  readonly onStatusChange: (value: StatusFilter) => void;
  readonly onUserChange: (value: string) => void;
}

/** The first row of the machines card: search, the status segments, the owner filter, add. */
export function MachinesToolbar({
  me,
  query,
  status,
  user,
  users,
  counts,
  onAddMachine,
  onQueryChange,
  onStatusChange,
  onUserChange,
}: MachinesToolbarProps): ReactElement {
  return (
    <TableToolbar actions={<AddMachine me={me} onAdd={onAddMachine} />}>
      <SearchInput value={query} placeholder="Search machines" onValueChange={onQueryChange} />
      <Tabs
        variant="segmented"
        size="sm"
        value={status}
        tabs={statusFilters.map((value) => ({
          value,
          label: <TabLabel label={statusFilterLabels[value]} count={counts[value]} />,
        }))}
        onValueChange={(value) => {
          onStatusChange(toStatusFilter(value));
        }}
      />
      {users === undefined ? null : (
        <Select
          size="sm"
          aria-label="Filter by user"
          className="w-40"
          value={user}
          items={userOptions(users)}
          onValueChange={(value) => {
            onUserChange(value ?? "");
          }}
        />
      )}
    </TableToolbar>
  );
}

function TabLabel({
  label,
  count,
}: {
  readonly label: string;
  readonly count: number;
}): ReactElement {
  return (
    <span className="flex items-center gap-1.5">
      {label}
      <span className="text-kumo-subtle tabular-nums">{count}</span>
    </span>
  );
}

/**
 * A machine joins by registering itself, so the console's part is handing out a pre-auth key. The
 * dialog belongs to the page, because the empty state opens the same one.
 */
function AddMachine({
  me,
  onAdd,
}: {
  readonly me: Me;
  readonly onAdd: () => void;
}): ReactElement | null {
  return can(me, "auth_keys") ? (
    <Button variant="primary" icon={PlusIcon} onClick={onAdd}>
      Add machine
    </Button>
  ) : null;
}

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}
