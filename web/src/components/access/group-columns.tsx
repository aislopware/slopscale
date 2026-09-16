import type { ReactElement } from "react";

import type { Group, Node } from "~/api/queries.ts";
import { GroupMenu } from "~/components/access/group-menu.tsx";
import {
  isBuiltin,
  isSelf,
  isSynced,
  machineCount,
  rulesUsingGroup,
} from "~/components/access/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { parseTime } from "~/lib/time.ts";

/** A group with its machine count resolved, so the table sorts by what it shows. */
export interface GroupRow extends Group {
  /** Machines the group resolves to, or null when the caller cannot list machines. */
  readonly machines: number | null;
}

export function toGroupRows(
  groups: readonly Group[],
  nodes: readonly Node[] | undefined,
): GroupRow[] {
  return groups.map((group) => ({
    ...group,
    machines: nodes === undefined ? null : machineCount(group, nodes),
  }));
}

const helper = createAppColumnHelper<GroupRow>();

export const groupColumns = helper.columns([
  helper.accessor((group) => `${group.name} ${group.description}`, {
    id: "name",
    header: "Group",
    enableSorting: true,
    cell: ({ row }) => <NameCell group={row.original} />,
    meta: { className: "w-[32%] min-w-52" },
  }),
  helper.accessor((group) => group.userIds.length, {
    id: "users",
    header: "Users",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => (
      <Count value={row.original.userIds.length} builtin={isBuiltin(row.original)} />
    ),
    meta: { className: "whitespace-nowrap", numeric: true },
  }),
  helper.accessor((group) => group.machines ?? -1, {
    id: "machines",
    header: "Machines",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <MachinesCell group={row.original} />,
    meta: { className: "whitespace-nowrap", numeric: true },
  }),
  helper.accessor((group) => group.id, {
    id: "rules",
    header: "Rules",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <Count value={rulesUsingGroup(table.options.meta?.rules ?? [], row.original).length} />
    ),
    meta: { className: "whitespace-nowrap", numeric: true },
  }),
  helper.accessor((group) => temporaryMembers(group).length, {
    id: "temporary",
    header: "Temporary",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <TemporaryCell group={row.original} />,
    meta: { className: "hidden whitespace-nowrap md:table-cell", numeric: true },
  }),
  helper.accessor((group) => group.createdAt, {
    id: "created",
    header: "Created",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) =>
      isBuiltin(row.original) ? (
        <span className="text-kumo-subtle">—</span>
      ) : (
        <span className="text-kumo-subtle">
          <RelativeTime value={row.original.createdAt} />
        </span>
      ),
    meta: { className: "hidden whitespace-nowrap lg:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, nodes, users, rules } = table.options.meta ?? {};

      return me === undefined || isBuiltin(row.original) ? null : (
        <GroupMenu
          group={row.original}
          nodes={nodes ?? []}
          users={users ?? []}
          rules={rules ?? []}
          me={me}
        />
      );
    },
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

function NameCell({ group }: { readonly group: Group }): ReactElement {
  const description = builtinDescription(group) ?? group.description;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate font-medium text-kumo-default">{group.name}</span>
        {isBuiltin(group) ? <span className="text-xs text-kumo-subtle">Built-in</span> : null}
        {isSynced(group) ? (
          <span title="Users follow the identity provider's groups claim">
            <span className="text-xs text-kumo-subtle">Synced</span>
          </span>
        ) : null}
      </span>
      {description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{description}</span>
      )}
    </div>
  );
}

function Count({
  value,
  builtin = false,
}: {
  readonly value: number;
  readonly builtin?: boolean;
}): ReactElement {
  // The builtin group has no member list of its own; a count would be misleading either way.
  if (builtin) {
    return <span className="text-kumo-subtle">—</span>;
  }

  return value === 0 ? (
    <span className="text-kumo-subtle">0</span>
  ) : (
    <span className="text-kumo-default">{value}</span>
  );
}

/** Direct members and the total once users' machines are counted, when the two differ. */
function MachinesCell({ group }: { readonly group: GroupRow }): ReactElement {
  if (group.machines === null) {
    return (
      <span className="text-kumo-subtle" title="Your credentials may not list machines">
        —
      </span>
    );
  }

  if (isSelf(group)) {
    return <span className="text-kumo-subtle">—</span>;
  }

  if (isBuiltin(group)) {
    return <span className="text-kumo-default">{group.machines}</span>;
  }

  const direct = group.nodeIds.length;

  return (
    <span className="flex min-w-0 flex-col items-end gap-0.5">
      <Count value={group.machines} />
      {group.machines === direct ? null : (
        <span className="text-xs text-kumo-subtle">{`${direct} direct`}</span>
      )}
    </span>
  );
}

/** The memberships of the group that end on their own, whether granted by hand or by a request. */
export function temporaryMembers(group: Group): Group["expiries"] {
  const now = new Date();

  return group.expiries.filter((expiry) => {
    const ends = parseTime(expiry.expiresAt);

    return ends !== null && ends > now;
  });
}

/** When the next temporary membership of the group runs out, or null when none does. */
function nextExpiry(group: Group): string | null {
  const [soonest] = temporaryMembers(group)
    .map((expiry) => expiry.expiresAt)
    .toSorted();

  return soonest ?? null;
}

/**
 * How many of the group's members are only there for a while, and when the first one goes. It is
 * the one thing about a group that changes without anybody touching it, so it is worth a column.
 */
function TemporaryCell({ group }: { readonly group: GroupRow }): ReactElement {
  const count = temporaryMembers(group).length;

  if (isBuiltin(group)) {
    return <span className="text-kumo-subtle">—</span>;
  }

  if (count === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  return (
    <span className="flex min-w-0 flex-col items-end gap-0.5">
      <span className="text-kumo-default">{count}</span>
      <span className="text-xs text-kumo-subtle">
        first <RelativeTime value={nextExpiry(group)} />
      </span>
    </span>
  );
}

/** What a builtin group stands for, or null for a group the operator described. */
function builtinDescription(group: Group): string | null {
  if (isSelf(group)) {
    return "As a destination, the machines owned by the source's own user.";
  }

  return isBuiltin(group) ? "Every machine in the tailnet, kept up to date by the server." : null;
}
