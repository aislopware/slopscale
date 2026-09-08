import { Badge } from "@cloudflare/kumo/components/badge";
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
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((group) => group.machines ?? -1, {
    id: "machines",
    header: "Machines",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <MachinesCell group={row.original} />,
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((group) => group.id, {
    id: "rules",
    header: "Rules",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row, table }) => (
      <Count value={rulesUsingGroup(table.options.meta?.rules ?? [], row.original).length} />
    ),
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((group) => group.createdAt, {
    id: "created",
    header: "Created",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) =>
      isBuiltin(row.original) ? (
        <span className="text-kumo-inactive">—</span>
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
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ group }: { readonly group: Group }): ReactElement {
  const description = builtinDescription(group) ?? group.description;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate font-medium text-kumo-default">{group.name}</span>
        {isBuiltin(group) ? <Badge variant="outline">Built in</Badge> : null}
        {isSynced(group) ? (
          <span title="Users follow the identity provider's groups claim">
            <Badge variant="outline">Synced</Badge>
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
    return <span className="text-kumo-inactive">—</span>;
  }

  return value === 0 ? (
    <span className="text-kumo-inactive">0</span>
  ) : (
    <span className="text-kumo-default">{value}</span>
  );
}

/** Direct members and the total once users' machines are counted, when the two differ. */
function MachinesCell({ group }: { readonly group: GroupRow }): ReactElement {
  if (group.machines === null) {
    return (
      <span className="text-kumo-inactive" title="You cannot list machines">
        —
      </span>
    );
  }

  if (isSelf(group)) {
    return <span className="text-kumo-inactive">—</span>;
  }

  if (isBuiltin(group)) {
    return <span className="text-kumo-default">{group.machines}</span>;
  }

  const direct = group.nodeIds.length;

  return (
    <span className="flex min-w-0 flex-col gap-0.5">
      <Count value={group.machines} />
      {group.machines === direct ? null : (
        <span className="text-xs text-kumo-subtle">{`${direct} direct`}</span>
      )}
    </span>
  );
}

/** What a builtin group stands for, or null for a group the operator described. */
function builtinDescription(group: Group): string | null {
  if (isSelf(group)) {
    return "In a rule's destination, the machines owned by the same user as the source. Tailscale's autogroup:self.";
  }

  return isBuiltin(group) ? "Every machine in the tailnet, kept up to date by the server." : null;
}
