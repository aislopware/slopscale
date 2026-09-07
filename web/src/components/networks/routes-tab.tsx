import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { SignpostIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo } from "react";
import type { ReactElement } from "react";

import type { Network, Node } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { toRouteRows } from "~/components/networks/model.ts";
import { routeColumns } from "~/components/networks/route-columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";

const emptyClass = "border-none bg-kumo-base [&>h2]:text-base";
const emptyIconSize = 32;

export interface RoutesTabProps {
  readonly me: Me;
  readonly nodes: readonly Node[];
  readonly networks: readonly Network[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Every route any machine advertises, with what approved it and a button for the rest. */
export function RoutesTab({
  me,
  nodes,
  networks,
  search,
  onSearchChange,
}: RoutesTabProps): ReactElement {
  const query = useDeferredValue(search);
  const rows = useMemo(() => toRouteRows(nodes, networks), [nodes, networks]);

  const table = useAppTable({
    data: rows,
    columns: routeColumns,
    getRowId: (row) => row.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "status", desc: false }] },
    meta: { me, networks },
  });

  const total = rows.length;
  const shown = table.getRowModel().rows.length;
  const pending = rows.filter((row) => row.status === "pending").length;

  return (
    <LayerCard className="overflow-hidden">
      <TableToolbar>
        <SearchInput
          value={search}
          placeholder="Search by route, machine or network"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <table.AppTable>
        <DataTable
          empty={
            total === 0 ? (
              <Empty
                className={emptyClass}
                size="sm"
                icon={<SignpostIcon size={emptyIconSize} />}
                title="Nothing advertised"
                description="No machine advertises a subnet or offers itself as an exit node. Run tailscale set --advertise-routes or --advertise-exit-node on one."
              />
            ) : (
              <Empty
                className={emptyClass}
                size="sm"
                title="No routes match"
                description="No route matches this search."
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
          footer={
            total === 0 ? undefined : (
              <TableFooter>{`Showing ${shown} of ${countRoutes(total)} · ${pending} pending`}</TableFooter>
            )
          }
        />
      </table.AppTable>
    </LayerCard>
  );
}

function countRoutes(total: number): string {
  return total === 1 ? "1 route" : `${total} routes`;
}
