import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { postureColumns } from "~/components/access/posture-columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";

export interface PosturesTabProps {
  readonly me: Me;
  readonly postures: readonly Posture[];
  readonly rules: readonly AccessRule[];
  readonly geoIpAvailable: boolean;
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Postures, the conditions a rule can require of the machine opening a connection. */
export function PosturesTab({
  me,
  postures,
  rules,
  geoIpAvailable,
  search,
  onSearchChange,
}: PosturesTabProps): ReactElement {
  const canEdit = can(me, "policy_file");
  const query = useDeferredValue(search);

  const table = useAppTable({
    data: postures,
    columns: postureColumns,
    getRowId: (posture) => posture.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, rules, geoIpAvailable },
  });

  const total = postures.length;

  return (
    <>
      <TableToolbar
        actions={
          <LinkButton
            href="/policy/postures/new"
            variant="primary"
            icon={PlusIcon}
            disabled={!canEdit}
          >
            New posture
          </LinkButton>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, description or expression"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <TableGroup>
        <DataTable
          table={table}
          empty={
            total === 0 ? (
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No postures yet"
                description="A posture names conditions a machine must meet. Attach it to a rule to require it of the sources."
              />
            ) : (
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No postures match"
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
            )
          }
        />
      </TableGroup>
    </>
  );
}
