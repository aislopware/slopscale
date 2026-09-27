import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import type { TrafficNode, TrafficReporter } from "~/api/traffic.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { BytesCell, VolumeCell } from "~/components/traffic/cells.tsx";
import { formatCount } from "~/components/traffic/format.ts";
import { MachineName } from "~/components/ui/machine-name.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { useWidths } from "~/lib/breakpoint.ts";
import type { Widths } from "~/lib/breakpoint.ts";

/**
 * Every gateway with what it carried in the window: the ones that carried something, then the
 * reporting ones that carried nothing, so a quiet gateway shows as quiet rather than missing.
 */
export function gatewayRows(
  carried: readonly TrafficNode[],
  reporters: readonly Pick<TrafficReporter, "nodeId" | "nodeName" | "nodeOwner">[],
): TrafficNode[] {
  const seen = new Set(carried.map((node) => node.nodeId));
  const quiet = reporters
    .filter((reporter) => !seen.has(reporter.nodeId))
    .map((reporter) => {
      const row: TrafficNode = {
        nodeId: reporter.nodeId,
        nodeName: reporter.nodeName,
        reporterIds: [],
        txBytes: 0,
        rxBytes: 0,
        txPackets: 0,
        rxPackets: 0,
        conns: 0,
      };

      if (reporter.nodeOwner !== undefined) {
        row.nodeOwner = reporter.nodeOwner;
      }

      return row;
    });

  return [...carried, ...quiet];
}

/** A gateway the server no longer knows keeps its traffic under its old id. */
export function gatewayName(node: { readonly nodeId: string; readonly nodeName: string }): string {
  return node.nodeName === "" ? `Removed gateway ${node.nodeId}` : node.nodeName;
}

function total(node: TrafficNode): number {
  return node.txBytes + node.rxBytes;
}

interface Row extends TrafficNode {
  readonly widest: number;
  readonly whole: number;
}

const helper = createAppColumnHelper<Row>();

function columns(widths: Widths): ReturnType<typeof helper.columns> {
  const gateway = helper.accessor((row) => gatewayName(row), {
    id: "gateway",
    header: "Gateway",
    enableSorting: true,
    cell: ({ row }) => (
      <MachineName
        name={
          <span
            className={cn(
              "max-w-full truncate",
              row.original.nodeName === "" ? "text-kumo-subtle" : "font-medium",
            )}
          >
            {gatewayName(row.original)}
          </span>
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
    gateway,
    volume,
    ...(widths.sm ? [upload, download] : []),
    ...(widths.md ? [conns] : []),
  ]);
}

/**
 * What each gateway carried in the window, busiest first. A row click narrows the page to that
 * gateway.
 */
export function GatewayTrafficTable({
  gateways,
  whole,
  footer,
  onPick,
}: {
  readonly gateways: readonly TrafficNode[];
  /** Upload plus download over the whole window. */
  readonly whole: number;
  readonly footer?: ReactNode;
  readonly onPick: (gatewayId: string) => void;
}): ReactElement {
  const widths = useWidths();
  const widest = Math.max(0, ...gateways.map((gateway) => total(gateway)));
  const rows: Row[] = gateways.map((gateway) => ({ ...gateway, widest, whole }));
  const table = useAppTable({
    data: rows,
    columns: columns(widths),
    getRowId: (row) => row.nodeId,
    initialState: { sorting: [{ id: "total", desc: true }] },
  });

  return (
    <table.AppTable>
      <DataTable
        empty={
          <SectionEmpty
            title="No gateway reports yet"
            description="Gateways show up here once their agent reports."
          />
        }
        footer={footer}
        onRowClick={onPick}
      />
    </table.AppTable>
  );
}
