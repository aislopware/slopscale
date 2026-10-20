import { Button } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupColumns, toGroupRows } from "~/components/access/group-columns.tsx";
import { GroupDialog } from "~/components/access/group-dialogs.tsx";
import { isBuiltin } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { plural } from "~/components/overview/plural.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

export interface GroupsTabProps {
  readonly me: Me;
  readonly groups: readonly Group[];
  readonly rules: readonly AccessRule[];
  /** Undefined while loading or when the caller may not list them; counts then show as unknown. */
  readonly nodes: readonly Node[] | undefined;
  readonly users: readonly User[] | undefined;
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Groups of machines, the nouns the rules are written in. */
export function GroupsTab({
  me,
  groups,
  rules,
  nodes,
  users,
  search,
  onSearchChange,
}: GroupsTabProps): ReactElement {
  const query = useDeferredValue(search);
  const rows = useMemo(() => toGroupRows(groups, nodes), [groups, nodes]);

  const table = useAppTable({
    data: rows,
    columns: groupColumns,
    getRowId: (group) => group.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: {
      me,
      rules,
      ...(nodes === undefined ? {} : { nodes }),
      ...(users === undefined ? {} : { users }),
    },
  });

  const own = groups.filter((group) => !isBuiltin(group)).length;
  const shown = table.getRowModel().rows.length;

  return (
    <>
      <TableToolbar>
        <SearchInput
          value={search}
          placeholder="Search by name or description"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <TableGroup>
        <DataTable
          table={table}
          empty={
            <SectionEmpty
              title="No groups match"
              contents={
                <Button
                  variant="secondary"
                  onClick={() => {
                    onSearchChange("");
                  }}
                >
                  Clear search
                </Button>
              }
            />
          }
          footer={
            <TableFooter>
              {own === 0
                ? "Only the built-in groups."
                : `Showing ${shown} of ${plural(groups.length, "group")}`}
            </TableFooter>
          }
        />
      </TableGroup>
    </>
  );
}

/**
 * The groups page's create action, in its page header beside the title as the dashboard's "Add a
 * policy" is, with the dialog it opens.
 */
export function NewGroupButton({
  me,
  nodes,
  users,
}: {
  readonly me: Me;
  readonly nodes: readonly Node[] | undefined;
  readonly users: readonly User[] | undefined;
}): ReactElement {
  const [creating, setCreating] = useState(false);
  const mutations = useAccessMutations();

  return (
    <>
      <Button
        variant="primary"
        icon={PlusIcon}
        disabled={!can(me, "policy_file")}
        onClick={() => {
          setCreating(true);
        }}
      >
        New group
      </Button>
      <GroupDialog
        nodes={nodes ?? []}
        users={users ?? []}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}
