import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import type { ExpandedState } from "@tanstack/react-table";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { Network, Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { routeColumns } from "~/components/networks/route-columns.tsx";
import { RouteFilterChips } from "~/components/networks/route-filters.tsx";
import {
  filterGroups,
  groupRoutes,
  isRouteGroup,
  noRouteFilters,
  routeFilterCounts,
} from "~/components/networks/routes-model.ts";
import type { RouteFilterState, RoutesRow } from "~/components/networks/routes-model.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface RoutesTabProps {
  readonly me: Me;
  readonly nodes: readonly Node[];
  readonly networks: readonly Network[];
  readonly search: string;
  readonly filters: RouteFilterState;
  readonly onSearchChange: (value: string) => void;
  readonly onFiltersChange: (value: RouteFilterState) => void;
}

/**
 * Every advertised route across the tailnet, one row per prefix. A prefix several machines
 * advertise unfolds into a row each, so a pair of subnet routers reads as one route with a standby
 * rather than as two rows that happen to share a number.
 */
export function RoutesTab({
  me,
  nodes,
  networks,
  search,
  filters,
  onSearchChange,
  onFiltersChange,
}: RoutesTabProps): ReactElement {
  const query = useDeferredValue(search);
  const groups = useMemo(() => groupRoutes(nodes, networks), [nodes, networks]);
  const counts = useMemo(() => routeFilterCounts(groups), [groups]);
  const rows: RoutesRow[] = useMemo(() => filterGroups(groups, filters), [groups, filters]);
  const [expanded, setExpanded] = useState<ExpandedState>({});

  const table = useAppTable({
    data: rows,
    columns: routeColumns,
    getRowId: (row) => row.id,
    // A prefix only one machine advertises stays a flat row; the rest carry their advertisers.
    getSubRows: (row) =>
      isRouteGroup(row) && row.advertisers.length > 1 ? row.advertisers : undefined,
    state: { globalFilter: query, expanded },
    onExpandedChange: setExpanded,
    initialState: { sorting: [{ id: "status", desc: false }] },
    meta: { me, networks },
  });

  const total = groups.length;
  const shown = table.getRowModel().rows.filter((row) => row.depth === 0).length;
  const pending = groups.reduce((sum, group) => sum + group.pending, 0);
  const narrowed = search !== "" || routeFilterCount(filters) > 0;

  return (
    <>
      <TableToolbar>
        <SearchInput
          value={search}
          placeholder="Search by route, machine or network"
          onValueChange={onSearchChange}
        />
        <RouteFilterChips
          state={filters}
          counts={counts}
          onToggle={(filter) => {
            onFiltersChange({ ...filters, [filter]: !filters[filter] });
          }}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="Nothing advertised"
                  contents={
                    <p className="max-w-140 text-center text-kumo-subtle">
                      No machine advertises a subnet or offers itself as an exit node. Run{" "}
                      <Code>tailscale set --advertise-routes</Code> or{" "}
                      <Code>--advertise-exit-node</Code> on one.
                    </p>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No routes match"
                  contents={
                    narrowed ? (
                      <Button
                        variant="secondary"
                        onClick={() => {
                          onSearchChange("");
                          onFiltersChange(noRouteFilters);
                        }}
                      >
                        Clear filters
                      </Button>
                    ) : undefined
                  }
                />
              )
            }
            footer={
              total === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${countRoutes(total)} · ${pending} pending`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
    </>
  );
}

function countRoutes(total: number): string {
  return total === 1 ? "1 route" : `${total} routes`;
}

function routeFilterCount(filters: RouteFilterState): number {
  return Object.values(filters).filter(Boolean).length;
}
