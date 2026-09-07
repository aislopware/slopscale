import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { HandWavingIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { requestColumns } from "~/components/access/request-columns.tsx";
import { requestNames, toRequestRows } from "~/components/access/request-model.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";

const emptyClass = "border-none bg-kumo-base [&>h2]:text-base";
const emptyIconSize = 32;

const filters = ["pending", "all"] as const;
type Filter = (typeof filters)[number];

const filterItems: readonly { value: Filter; label: string }[] = [
  { value: "pending", label: "Pending" },
  { value: "all", label: "All" },
];

export interface RequestsTabProps {
  readonly me: Me;
  readonly requests: readonly AccessRequest[];
  readonly canDecide: boolean;
  readonly groups: readonly Group[];
  /** Undefined when the caller may not list them; names then fall back to ids. */
  readonly users: readonly User[] | undefined;
  readonly nodes: readonly Node[] | undefined;
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** The asks for temporary access, pending ones first, for an approver to decide. */
export function RequestsTab({
  me,
  requests,
  canDecide,
  groups,
  users,
  nodes,
  search,
  onSearchChange,
}: RequestsTabProps): ReactElement {
  const query = useDeferredValue(search);
  const [filter, setFilter] = useState<Filter>("pending");
  const rows = useMemo(
    () => toRequestRows(requests, requestNames(groups, users, nodes)),
    [requests, groups, users, nodes],
  );
  const shownRows = useMemo(
    () => (filter === "pending" ? rows.filter((row) => row.status === "pending") : rows),
    [rows, filter],
  );

  const table = useAppTable({
    data: shownRows,
    columns: requestColumns,
    getRowId: (request) => request.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, canDecide },
  });

  const shown = table.getRowModel().rows.length;
  const pending = rows.filter((row) => row.status === "pending").length;

  return (
    <LayerCard className="overflow-hidden">
      <TableToolbar
        actions={
          <Tabs
            variant="segmented"
            tabs={[...filterItems]}
            value={filter}
            onValueChange={(value) => {
              setFilter(filters.find((known) => known === value) ?? "pending");
            }}
          />
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by requester, machine, group or reason"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <table.AppTable>
        <DataTable
          empty={
            <Empty
              className={emptyClass}
              size="sm"
              icon={<HandWavingIcon size={emptyIconSize} />}
              title={filter === "pending" ? "Nothing to decide" : "No requests"}
              description={
                filter === "pending"
                  ? "No request is waiting. Members ask under My access, for groups marked as requestable."
                  : "No request matches this search."
              }
              contents={
                search === "" ? undefined : (
                  <Button
                    variant="secondary"
                    onClick={() => {
                      onSearchChange("");
                    }}
                  >
                    Clear search
                  </Button>
                )
              }
            />
          }
          footer={
            rows.length === 0 ? undefined : (
              <TableFooter>
                {`Showing ${shown} of ${rows.length === 1 ? "1 request" : `${rows.length} requests`} · ${pending} pending`}
              </TableFooter>
            )
          }
        />
      </table.AppTable>
    </LayerCard>
  );
}
