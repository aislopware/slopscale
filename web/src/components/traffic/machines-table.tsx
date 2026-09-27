import { Link, useNavigate } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { MachineOwner } from "~/api/schema.gen.ts";
import type { TrafficNode } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { BytesCell, VolumeCell } from "~/components/traffic/cells.tsx";
import { formatCount } from "~/components/traffic/format.ts";
import { windowOf } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import { MachineName } from "~/components/ui/machine-name.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { useWidths } from "~/lib/breakpoint.ts";
import type { Widths } from "~/lib/breakpoint.ts";
import { machineLabel } from "~/lib/node.ts";

/** A machine the server no longer knows keeps its traffic under its old id. */
export function trafficNodeName(node: {
  readonly nodeId: string;
  readonly nodeName: string;
}): string {
  return node.nodeName === "" ? `Removed machine ${node.nodeId}` : node.nodeName;
}

/** The machine and whose it is, for a line of text; a removed machine has no owner left. */
export function trafficNodeLabel(node: {
  readonly nodeId: string;
  readonly nodeName: string;
  readonly nodeOwner?: MachineOwner;
}): string {
  return machineLabel(trafficNodeName(node), node.nodeOwner);
}

function total(node: TrafficNode): number {
  return node.txBytes + node.rxBytes;
}

interface Row extends TrafficNode {
  /** The busiest machine's total, which fills the bar. */
  readonly widest: number;
  /** Everything in the window. */
  readonly whole: number;
}

const helper = createAppColumnHelper<Row>();

function columns(search: TrafficWindowSearch, widths: Widths): ReturnType<typeof helper.columns> {
  const machine = helper.accessor((row) => trafficNodeLabel(row), {
    id: "machine",
    header: "Machine",
    enableSorting: true,
    cell: ({ row }) => (
      <MachineName
        name={
          <Link
            to="/traffic/machines/$nodeId"
            params={{ nodeId: row.original.nodeId }}
            search={{ ...windowOf(search), by: "host" }}
            className={
              row.original.nodeName === ""
                ? "max-w-full truncate text-kumo-subtle hover:underline"
                : "max-w-full truncate font-medium text-kumo-default hover:underline"
            }
          >
            {trafficNodeName(row.original)}
          </Link>
        }
        owner={row.original.nodeOwner}
      />
    ),
    meta: { className: "w-[30%] max-w-0 min-w-40 truncate" },
  });
  const volume = helper.accessor((row) => total(row), {
    id: "total",
    header: "Total",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => (
      <VolumeCell
        bytes={total(row.original)}
        widest={row.original.widest}
        whole={row.original.whole}
      />
    ),
    meta: { numeric: true },
  });
  const upload = helper.accessor((row) => row.txBytes, {
    id: "upload",
    header: "Upload",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <BytesCell bytes={row.original.txBytes} />,
    meta: { numeric: true },
  });
  const download = helper.accessor((row) => row.rxBytes, {
    id: "download",
    header: "Download",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <BytesCell bytes={row.original.rxBytes} />,
    meta: { numeric: true },
  });
  const conns = helper.accessor((row) => row.conns, {
    id: "conns",
    header: "Connections",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <span className="tabular-nums">{formatCount(row.original.conns)}</span>,
    meta: { numeric: true, className: "text-kumo-subtle" },
  });

  return helper.columns([
    machine,
    volume,
    ...(widths.sm ? [upload, download] : []),
    ...(widths.md ? [conns] : []),
  ]);
}

/**
 * The machines that sent the most through the gateways, busiest first. The bar is against the
 * busiest machine on the list; hovering the total gives its share of the whole window.
 */
export function MachinesTable({
  nodes,
  whole,
  search,
  footer,
  empty,
}: {
  readonly nodes: readonly TrafficNode[];
  /** Upload plus download over the whole window. */
  readonly whole: number;
  readonly search: TrafficWindowSearch;
  /** The band under the table; a count by default. */
  readonly footer?: ReactNode;
  readonly empty?: ReactNode;
}): ReactElement {
  const navigate = useNavigate();
  const widths = useWidths();
  const widest = Math.max(0, ...nodes.map((node) => total(node)));
  const rows: Row[] = nodes.map((node) => ({ ...node, widest, whole }));
  const table = useAppTable({
    data: rows,
    columns: columns(search, widths),
    getRowId: (row) => row.nodeId,
    initialState: { sorting: [{ id: "total", desc: true }] },
  });

  return (
    <table.AppTable>
      <DataTable
        empty={
          empty ?? (
            <SectionEmpty
              title="No traffic in this window"
              description="Machines show up here once a gateway reports traffic they sent through it."
            />
          )
        }
        footer={
          footer ??
          (nodes.length === 0 ? undefined : (
            <TableFooter>{`Showing ${plural(nodes.length, "machine")}`}</TableFooter>
          ))
        }
        onRowClick={(nodeId) => {
          void navigate({
            to: "/traffic/machines/$nodeId",
            params: { nodeId },
            search: { ...windowOf(search), by: "host" },
          });
        }}
      />
    </table.AppTable>
  );
}
