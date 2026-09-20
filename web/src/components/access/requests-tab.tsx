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
import { useNow } from "~/lib/use-now.ts";

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

/** What an empty tab says, when nothing was searched for. */
const emptyText: Record<Filter, { title: string; description: string }> = {
  pending: {
    title: "Nothing to decide",
    description: "No request is waiting. Users ask under My access, for groups marked requestable.",
  },
  active: {
    title: "No access in effect",
    description:
      "No approved request is in effect. A membership an operator granted by hand is not a request and is not listed here.",
  },
  all: { title: "No requests", description: "Nobody has asked for temporary access yet." },
};

/**
 * Why the list is empty. A search that matched nothing says so, rather than claiming the tab is
 * empty: the tab's own count is right there beside it, and the two would contradict each other.
 */
function emptyFor(filter: Filter, searching: boolean): { title: string; description: string } {
  return searching
    ? { title: "No matches", description: "No request here matches this search." }
    : emptyText[filter];
}

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
  // A grant ends on its own, so the phases are re-read on a clock: without one a page left open
  // keeps a finished grant under "In effect" and goes on offering to revoke it.
  const now = useNow();
  const rows = useMemo(
    () => toRequestRows(requests, requestNames(groups, users, nodes), now),
    [requests, groups, users, nodes, now],
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
                title={emptyFor(filter, search !== "").title}
                description={emptyFor(filter, search !== "").description}
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
                  {`Showing ${shown} of ${rows.length === 1 ? "1 request" : `${rows.length} requests`}`}
                </TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
    </>
  );
}
