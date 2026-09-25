import type { ReactElement, ReactNode } from "react";

import type { NameGrouping, TrafficName } from "~/api/traffic.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { formatCount, shareLabel } from "~/components/traffic/format.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";

/** A share of failed answers worth drawing the eye to: a name that mostly does not resolve. */
const failingShare = 0.5;

export function nameKey(row: TrafficName, groupBy: NameGrouping): string {
  return groupBy === "node" ? row.nodeId : row.name;
}

function NameCell({ row }: { readonly row: TrafficName }): ReactElement {
  if (row.name === "") {
    return <span className="text-kumo-subtle">Everything else</span>;
  }

  return (
    <span className="block truncate font-mono text-[0.9em]" title={row.name}>
      {row.name}
    </span>
  );
}

function FailedCell({ row }: { readonly row: TrafficName }): ReactElement {
  if (row.failed === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  const failing = row.queries > 0 && row.failed / row.queries >= failingShare;

  return (
    <Status tone={failing ? "warning" : "neutral"} className="tabular-nums">
      {`${formatCount(row.failed)} (${shareLabel(row.failed, row.queries)})`}
    </Status>
  );
}

const helper = createAppColumnHelper<TrafficName>();

function columnsFor(groupBy: NameGrouping, oneMachine: boolean): ReturnType<typeof helper.columns> {
  const key =
    groupBy === "node"
      ? helper.accessor((row) => trafficNodeName(row), {
          id: "key",
          header: "Machine",
          enableSorting: true,
          cell: ({ row }) => (
            <span className={row.original.nodeName === "" ? "text-kumo-subtle" : "font-medium"}>
              {trafficNodeName(row.original)}
            </span>
          ),
          meta: { className: "w-[45%] max-w-0 min-w-48" },
        })
      : helper.accessor((row) => row.name, {
          id: "key",
          header: "Name",
          enableSorting: true,
          cell: ({ row }) => <NameCell row={row.original} />,
          meta: { className: "w-[45%] max-w-0 min-w-48" },
        });

  return helper.columns([
    key,
    helper.accessor((row) => row.queries, {
      id: "queries",
      header: "Lookups",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <span className="tabular-nums">{formatCount(row.original.queries)}</span>,
      meta: { numeric: true },
    }),
    helper.accessor((row) => row.failed, {
      id: "failed",
      header: "Failed",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <FailedCell row={row.original} />,
      meta: { numeric: true },
    }),
    ...(groupBy === "name" && !oneMachine
      ? [
          helper.accessor((row) => row.nodes, {
            id: "machines",
            header: "Machines",
            enableSorting: true,
            sortDescFirst: true,
            cell: ({ row }) => (
              <span className="text-kumo-subtle tabular-nums">{row.original.nodes}</span>
            ),
            meta: { numeric: true, className: "hidden sm:table-cell" },
          }),
        ]
      : []),
  ]);
}

/** The names machines looked up through the gateways' resolvers, or the machines that looked. */
export function NamesTable({
  rows,
  groupBy,
  footer,
  empty,
  oneMachine = false,
  onPick,
}: {
  readonly rows: readonly TrafficName[];
  readonly groupBy: NameGrouping;
  /** The rows are one machine's lookups, so the machine count is always one. */
  readonly oneMachine?: boolean;
  readonly footer?: ReactNode;
  readonly empty?: ReactNode;
  readonly onPick?: (row: TrafficName) => void;
}): ReactElement {
  const byId = new Map(rows.map((row) => [nameKey(row, groupBy), row]));
  const table = useAppTable({
    data: rows,
    columns: columnsFor(groupBy, oneMachine),
    getRowId: (row) => nameKey(row, groupBy),
    initialState: { sorting: [{ id: "queries", desc: true }] },
  });

  return (
    <table.AppTable>
      <DataTable
        empty={
          empty ?? (
            <SectionEmpty
              title="No lookups in this window"
              description="Names show up here once machines resolve them through a gateway's resolver."
            />
          )
        }
        footer={footer}
        onRowClick={
          onPick === undefined
            ? undefined
            : (id) => {
                const row = byId.get(id);

                if (row !== undefined && (groupBy === "node" || row.name !== "")) {
                  onPick(row);
                }
              }
        }
      />
    </table.AppTable>
  );
}
