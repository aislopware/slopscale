import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupColumns, toGroupRows } from "~/components/access/group-columns.tsx";
import { isBuiltin } from "~/components/access/model.ts";
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

  return (
    <>
      <TableToolbar actions={<NewGroupButton me={me} />}>
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
          // With no groups of the tailnet's own the list is the built-in ones alone, which the band
          // says instead of a range.
          footer={own === 0 ? <TableFooter>Only the built-in groups.</TableFooter> : undefined}
        />
      </TableGroup>
    </>
  );
}

/** The groups list's create action, right of the search. It opens the group editor page. */
function NewGroupButton({ me }: { readonly me: Me }): ReactElement {
  return (
    <LinkButton
      href="/policy/groups/new"
      variant="primary"
      icon={PlusIcon}
      disabled={!can(me, "policy_file")}
    >
      New group
    </LinkButton>
  );
}
