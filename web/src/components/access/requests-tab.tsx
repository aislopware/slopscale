import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { requestColumns } from "~/components/access/request-columns.tsx";
import { requestNames, toRequestRows } from "~/components/access/request-model.ts";
import type { RequestRow } from "~/components/access/request-model.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { countedTabs } from "~/components/table/tab-count.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

const filters = ["pending", "active", "all"] as const;
type Filter = (typeof filters)[number];

const filterItems: readonly { value: Filter; label: string }[] = [
  { value: "pending", label: "Pending" },
  { value: "active", label: "In effect" },
  { value: "all", label: "All" },
];

/** The rows a filter shows; "all" keeps the whole record, outcomes included. */
function rowsFor(rows: readonly RequestRow[], filter: Filter): RequestRow[] {
  if (filter === "all") {
    return [...rows];
  }

  return rows.filter((row) => row.phase === filter);
}

/** What an empty list says, which depends on why it is empty. */
const emptyText: Record<Filter, { title: string; description: string }> = {
  pending: {
    title: "Nothing to decide",
    description: "No request is waiting. Users ask under My access, for groups marked requestable.",
  },
  active: {
    title: "No access in effect",
    description:
      "Nobody holds temporary access right now. Approved grants show here until they run out.",
  },
  all: { title: "No requests", description: "No request matches this search." },
};

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
  const shownRows = useMemo(() => rowsFor(rows, filter), [rows, filter]);

  const table = useAppTable({
    data: shownRows,
    columns: requestColumns,
    getRowId: (request) => request.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, canDecide },
  });

  const shown = table.getRowModel().rows.length;
  const pending = rows.filter((row) => row.phase === "pending").length;
  const active = rows.filter((row) => row.phase === "active").length;
  const counts: Record<Filter, number> = { pending, active, all: rows.length };

  return (
    <>
      <TableToolbar>
        <SearchInput
          value={search}
          placeholder="Search by requester, machine, group or reason"
          onValueChange={onSearchChange}
        />
        <Tabs
          variant="segmented"
          tabs={countedTabs(filterItems, (value) => counts[value])}
          value={filter}
          onValueChange={(value) => {
            setFilter(filters.find((known) => known === value) ?? "pending");
          }}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              <Empty
                className={tableEmptyClass}
                size="sm"
                title={emptyText[filter].title}
                description={emptyText[filter].description}
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
                  {`Showing ${shown} of ${rows.length === 1 ? "1 request" : `${rows.length} requests`} · ${pending} pending · ${active} in effect`}
                </TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
    </>
  );
}
