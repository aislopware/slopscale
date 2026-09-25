import type { ReactElement, ReactNode } from "react";

import type { NameGrouping, TrafficName } from "~/api/traffic.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { formatCount, shareLabel } from "~/components/traffic/format.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { Status } from "~/components/ui/status.tsx";
import { useWidths } from "~/lib/breakpoint.ts";
import type { Widths } from "~/lib/breakpoint.ts";

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
    <span className="flex min-w-0">
      <Domain domain={row.name} />
    </span>
  );
}

/** The failed answers, with their share of the lookups where there is room for it. */
function FailedCell({
  row,
  withShare,
}: {
  readonly row: TrafficName;
  readonly withShare: boolean;
}): ReactElement {
  if (row.failed === 0) {
    return <span className="text-kumo-subtle">0</span>;
  }

  const failing = row.queries > 0 && row.failed / row.queries >= failingShare;
  const count = formatCount(row.failed);

  return (
    <Status tone={failing ? "warning" : "neutral"} className="tabular-nums">
      {withShare ? `${count} (${shareLabel(row.failed, row.queries)})` : count}
    </Status>
  );
}

const helper = createAppColumnHelper<TrafficName>();

function columnsFor(
  groupBy: NameGrouping,
  oneMachine: boolean,
  widths: Widths,
): ReturnType<typeof helper.columns> {
  // A phone keeps the name's column narrow enough that the figures beside it still fit across.
  const keyClass = "w-[45%] max-w-0 min-w-32 sm:min-w-48";
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
          meta: { className: `${keyClass} truncate` },
        })
      : helper.accessor((row) => row.name, {
          id: "key",
          header: "Name",
          enableSorting: true,
          cell: ({ row }) => <NameCell row={row.original} />,
          meta: { className: keyClass },
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
      cell: ({ row }) => <FailedCell row={row.original} withShare={widths.sm} />,
      meta: { numeric: true },
    }),
    ...(groupBy === "name" && !oneMachine && widths.sm
      ? [
          helper.accessor((row) => row.nodes, {
            id: "machines",
            header: "Machines",
            enableSorting: true,
            sortDescFirst: true,
            cell: ({ row }) => (
              <span className="text-kumo-subtle tabular-nums">{row.original.nodes}</span>
            ),
            meta: { numeric: true },
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
  const widths = useWidths();
  const byId = new Map(rows.map((row) => [nameKey(row, groupBy), row]));
  const pickable = (id: string): TrafficName | undefined => {
    const row = byId.get(id);

    // The folded remainder stands for no one name.
    return row === undefined || (groupBy === "name" && row.name === "") ? undefined : row;
  };
  const table = useAppTable({
    data: rows,
    columns: columnsFor(groupBy, oneMachine, widths),
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
                const row = pickable(id);

                if (row !== undefined) {
                  onPick(row);
                }
              }
        }
        isRowClickable={(id) => pickable(id) !== undefined}
      />
    </table.AppTable>
  );
}
