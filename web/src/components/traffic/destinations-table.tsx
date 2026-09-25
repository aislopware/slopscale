import type { ReactElement, ReactNode } from "react";

import type { DestinationGrouping, TrafficDestination } from "~/api/traffic.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { BytesCell, VolumeCell } from "~/components/traffic/cells.tsx";
import {
  countryName,
  formatCount,
  networkLabel,
  portLabel,
  serviceName,
} from "~/components/traffic/format.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

/** What the rows of each grouping are, for the first column's heading and the counts. */
export const groupingLabels: Record<DestinationGrouping, string> = {
  host: "Host",
  destination: "Address",
  asn: "Network",
  country: "Country",
  port: "Port",
  node: "Machine",
  reporter: "Gateway",
};

const keys: Record<DestinationGrouping, (row: TrafficDestination) => string> = {
  host: (row) => row.host,
  destination: (row) => `${row.dst}|${row.proto}|${row.port}|${row.host}`,
  asn: (row) => String(row.asn),
  country: (row) => row.country,
  port: (row) => `${row.proto}:${row.port}`,
  node: (row) => row.nodeId,
  reporter: (row) => row.nodeId,
};

/** The key a row stands for in its grouping; the same row read twice gets the same id. */
export function destinationKey(row: TrafficDestination, groupBy: DestinationGrouping): string {
  return keys[groupBy](row);
}

const never = (): boolean => false;

const remainders: Record<DestinationGrouping, (row: TrafficDestination) => boolean> = {
  host: (row) => row.host === "",
  destination: (row) => row.dst === "",
  port: (row) => row.proto === 0,
  asn: (row) => row.asn === 0,
  country: (row) => row.country === "",
  node: never,
  reporter: never,
};

/**
 * Whether the row stands for no one value: the remainder the server folds the smaller destinations
 * of a busy hour into, or the addresses with no known network or country. Nothing narrows to it.
 */
export function isRemainder(row: TrafficDestination, groupBy: DestinationGrouping): boolean {
  return remainders[groupBy](row);
}

const remainderLabel = "Everything else";
const unknownLabel = "Unknown";

function Subtle({ children }: { readonly children: ReactNode }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

function HostCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.host === "") {
    return <Subtle>{remainderLabel}</Subtle>;
  }

  return (
    <span className="flex min-w-0 items-baseline gap-2">
      <span className="truncate font-mono text-[0.9em]" title={row.host}>
        {row.host}
      </span>
      {row.private ? <Subtle>LAN</Subtle> : null}
    </span>
  );
}

function AddressCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.dst === "") {
    return <Subtle>{remainderLabel}</Subtle>;
  }

  return (
    <span className="flex min-w-0 flex-col">
      <span className="flex min-w-0 items-baseline gap-2">
        <span className="truncate font-mono text-[0.9em]">{row.dst}</span>
        {row.private ? <Subtle>LAN</Subtle> : null}
      </span>
      {row.host === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle" title={row.host}>
          {row.host}
        </span>
      )}
    </span>
  );
}

function NetworkCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.asn === 0) {
    return <Subtle>{row.private ? "Private network" : unknownLabel}</Subtle>;
  }

  return (
    <span className="block truncate" title={networkLabel(row.asn, row.asName)}>
      {row.asName === "" ? `AS${row.asn}` : row.asName}
      <span className="ms-2 text-xs text-kumo-subtle">AS{row.asn}</span>
    </span>
  );
}

function CountryCell({ code }: { readonly code: string }): ReactElement {
  return code === "" ? <Subtle>{unknownLabel}</Subtle> : <span>{countryName(code)}</span>;
}

function PortCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.proto === 0) {
    return <Subtle>{remainderLabel}</Subtle>;
  }

  const service = serviceName(row.proto, row.port);

  return (
    <span className="whitespace-nowrap">
      {portLabel(row.proto, row.port)}
      {service === "" ? null : <span className="ms-2 text-kumo-subtle">{service}</span>}
    </span>
  );
}

function NodeCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  return (
    <span className={row.nodeName === "" ? "text-kumo-subtle" : "font-medium"}>
      {trafficNodeName(row)}
    </span>
  );
}

const keyCells: Record<
  DestinationGrouping,
  (props: { readonly row: TrafficDestination }) => ReactElement
> = {
  host: HostCell,
  destination: AddressCell,
  asn: NetworkCell,
  country: ({ row }) => <CountryCell code={row.country} />,
  port: PortCell,
  node: NodeCell,
  reporter: NodeCell,
};

function KeyCell({
  row,
  groupBy,
}: {
  readonly row: TrafficDestination;
  readonly groupBy: DestinationGrouping;
}): ReactElement {
  const Cell = keyCells[groupBy];

  return <Cell row={row} />;
}

interface Row extends TrafficDestination {
  readonly widest: number;
  readonly whole: number;
}

const helper = createAppColumnHelper<Row>();

function total(row: TrafficDestination): number {
  return row.txBytes + row.rxBytes;
}

/** The columns for a grouping: its key first, then what the grouping does not already say. */
function columnsFor(
  groupBy: DestinationGrouping,
  oneMachine: boolean,
): ReturnType<typeof helper.columns> {
  const key = helper.accessor((row) => destinationKey(row, groupBy), {
    id: "key",
    header: groupingLabels[groupBy],
    enableSorting: true,
    cell: ({ row }) => <KeyCell row={row.original} groupBy={groupBy} />,
    meta: { className: "w-[34%] max-w-0 min-w-48" },
  });
  const network = helper.accessor((row) => row.asName, {
    id: "network",
    header: "Network",
    enableSorting: true,
    cell: ({ row }) => <NetworkCell row={row.original} />,
    meta: { className: "hidden lg:table-cell max-w-0 w-[20%] min-w-36" },
  });
  const country = helper.accessor((row) => countryName(row.country), {
    id: "country",
    header: "Country",
    enableSorting: true,
    cell: ({ row }) => <CountryCell code={row.original.country} />,
    meta: { className: "hidden xl:table-cell whitespace-nowrap" },
  });
  const port = helper.accessor((row) => row.port, {
    id: "port",
    header: "Port",
    enableSorting: true,
    cell: ({ row }) => <PortCell row={row.original} />,
    meta: { className: "hidden md:table-cell" },
  });
  const machines = helper.accessor((row) => row.nodes, {
    id: "machines",
    header: "Machines",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <span className="tabular-nums">{row.original.nodes}</span>,
    meta: { numeric: true, className: "hidden md:table-cell text-kumo-subtle" },
  });
  const counts = [
    helper.accessor((row) => total(row), {
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
    }),
    helper.accessor((row) => row.txBytes, {
      id: "upload",
      header: "Upload",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <BytesCell bytes={row.original.txBytes} />,
      meta: { numeric: true, className: "hidden sm:table-cell" },
    }),
    helper.accessor((row) => row.rxBytes, {
      id: "download",
      header: "Download",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <BytesCell bytes={row.original.rxBytes} />,
      meta: { numeric: true, className: "hidden sm:table-cell" },
    }),
    helper.accessor((row) => row.conns, {
      id: "conns",
      header: "Connections",
      enableSorting: true,
      sortDescFirst: true,
      cell: ({ row }) => <span className="tabular-nums">{formatCount(row.original.conns)}</span>,
      meta: { numeric: true, className: "hidden xl:table-cell text-kumo-subtle" },
    }),
  ];

  if (groupBy === "destination") {
    return helper.columns([key, network, country, port, ...counts]);
  }

  // A row of machines or gateways is one machine, and so is every row of a view narrowed to one;
  // otherwise a row gathers several.
  return oneMachine || groupBy === "node" || groupBy === "reporter"
    ? helper.columns([key, ...counts])
    : helper.columns([key, machines, ...counts]);
}

/**
 * Where the traffic went, grouped the way the page asks: by host name, by address, by network, by
 * country, by port, or by the machines and gateways that carried it. A row click narrows the page
 * to that row, which is how an operator gets from "who uses YouTube" to "which machines".
 */
export function DestinationsTable({
  rows,
  groupBy,
  whole,
  footer,
  empty,
  oneMachine = false,
  onPick,
}: {
  readonly rows: readonly TrafficDestination[];
  readonly groupBy: DestinationGrouping;
  /** The rows are one machine's, so how many machines each reached goes without saying. */
  readonly oneMachine?: boolean;
  /** Upload plus download of everything the rows were drawn from. */
  readonly whole: number;
  readonly footer?: ReactNode;
  readonly empty?: ReactNode;
  /** Called with the clicked row; a folded remainder row is not clickable. */
  readonly onPick?: (row: TrafficDestination) => void;
}): ReactElement {
  const widest = Math.max(0, ...rows.map((row) => total(row)));
  const data: Row[] = rows.map((row) => ({ ...row, widest, whole }));
  const byId = new Map(rows.map((row) => [destinationKey(row, groupBy), row]));
  const table = useAppTable({
    data,
    columns: columnsFor(groupBy, oneMachine),
    getRowId: (row) => destinationKey(row, groupBy),
    initialState: { sorting: [{ id: "total", desc: true }] },
  });

  return (
    <table.AppTable>
      <DataTable
        empty={
          empty ?? (
            <SectionEmpty
              title="No destinations in this window"
              description="Gateways report where machines connect once traffic passes through them."
            />
          )
        }
        footer={footer}
        onRowClick={
          onPick === undefined
            ? undefined
            : (id) => {
                const row = byId.get(id);

                if (row !== undefined && !isRemainder(row, groupBy)) {
                  onPick(row);
                }
              }
        }
      />
    </table.AppTable>
  );
}
