import type { ReactElement, ReactNode } from "react";

import type { DestinationGrouping, TrafficDestination } from "~/api/traffic.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { BytesCell, VolumeCell } from "~/components/traffic/cells.tsx";
import {
  countryName,
  formatCount,
  networkLabel,
  networkName,
  portLabel,
  serviceName,
} from "~/components/traffic/format.ts";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { Domain } from "~/components/ui/domain.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { useWidths } from "~/lib/breakpoint.ts";
import type { Widths } from "~/lib/breakpoint.ts";
import { isIp } from "~/lib/ip.ts";

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

// The server keeps private destinations apart from public ones of the same network or country, so
// the flag is part of those keys.
const keys: Record<DestinationGrouping, (row: TrafficDestination) => string> = {
  host: (row) => row.host,
  destination: (row) => `${row.dst}|${row.proto}|${row.port}|${row.host}`,
  asn: (row) => `${row.asn}|${String(row.private)}`,
  country: (row) => `${row.country}|${String(row.private)}`,
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
  asn: (row) => row.asn === 0 && !row.private,
  country: (row) => row.country === "" && !row.private,
  node: never,
  reporter: never,
};

/**
 * Whether the row stands for no one value: the remainder the server folds the smaller destinations
 * of a busy hour into, or the public addresses with no known network or country. Nothing narrows to
 * it; the private ones open as the LAN.
 */
export function isRemainder(row: TrafficDestination, groupBy: DestinationGrouping): boolean {
  return remainders[groupBy](row);
}

const remainderLabel = "Everything else";
const unknownLabel = "Unknown";

function Subtle({ children }: { readonly children: ReactNode }): ReactElement {
  return <span className="text-kumo-subtle">{children}</span>;
}

/** A name as a domain token; an address, which has no site to tint by, in plain code type. */
function HostName({ name }: { readonly name: string }): ReactElement {
  return isIp(name) ? (
    <span className="truncate font-mono text-[0.9em]" title={name}>
      {name}
    </span>
  ) : (
    <Domain domain={name} />
  );
}

function HostCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.host === "") {
    return <Subtle>{remainderLabel}</Subtle>;
  }

  return (
    <span className="flex min-w-0 items-center gap-2">
      <HostName name={row.host} />
      {row.private ? <Subtle>LAN</Subtle> : null}
    </span>
  );
}

function AddressCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.dst === "") {
    return <Subtle>{remainderLabel}</Subtle>;
  }

  return (
    <span className="flex min-w-0 flex-col items-start gap-1">
      <span className="flex max-w-full min-w-0 items-center gap-2">
        <span className="truncate font-mono text-[0.9em]">{row.dst}</span>
        {row.private ? <Subtle>LAN</Subtle> : null}
      </span>
      {row.host === "" || row.host === row.dst ? null : (
        <Domain domain={row.host} className="text-xs" />
      )}
    </span>
  );
}

/** The organisation leads; the registry handle and the number are there for whoever needs them. */
function NetworkName({
  row,
  withHandle,
}: {
  readonly row: TrafficDestination;
  readonly withHandle: boolean;
}): ReactElement {
  const { org, handle } = networkName(row.asName);

  return (
    <span className="block truncate" title={networkLabel(row.asn, row.asName)}>
      {org === "" ? `AS${row.asn}` : org}
      <span className="ms-2 text-xs text-kumo-subtle">
        {withHandle && handle !== "" ? `${handle} · AS${row.asn}` : `AS${row.asn}`}
      </span>
    </span>
  );
}

function NetworkCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.asn === 0) {
    return row.private ? <span>LAN</span> : <Subtle>{unknownLabel}</Subtle>;
  }

  return <NetworkName row={row} withHandle />;
}

function CountryCell({ row }: { readonly row: TrafficDestination }): ReactElement {
  if (row.country !== "") {
    return <span>{countryName(row.country)}</span>;
  }

  return row.private ? <span>LAN</span> : <Subtle>{unknownLabel}</Subtle>;
}

/**
 * An address's network beside it, without the registry handle the column has no room for. A LAN
 * address, already marked so, has none to show.
 */
function NetworkColumnCell({ row }: { readonly row: TrafficDestination }): ReactElement | null {
  if (row.asn === 0) {
    return row.private ? null : <Subtle>{unknownLabel}</Subtle>;
  }

  return <NetworkName row={row} withHandle={false} />;
}

function CountryColumnCell({ row }: { readonly row: TrafficDestination }): ReactElement | null {
  return row.private && row.country === "" ? null : <CountryCell row={row} />;
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
  country: CountryCell,
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
  readonly of: string | undefined;
}

const helper = createAppColumnHelper<Row>();

function total(row: TrafficDestination): number {
  return row.txBytes + row.rxBytes;
}

/**
 * The columns for a grouping and the width at hand: its key first, then what the grouping does not
 * already say, dropping the least telling ones as the screen narrows.
 */
function columnsFor(
  groupBy: DestinationGrouping,
  oneMachine: boolean,
  widths: Widths,
): ReturnType<typeof helper.columns> {
  const key = helper.accessor((row) => destinationKey(row, groupBy), {
    id: "key",
    header: groupingLabels[groupBy],
    enableSorting: true,
    cell: ({ row }) => <KeyCell row={row.original} groupBy={groupBy} />,
    meta: { className: "w-[34%] max-w-0 min-w-40 max-sm:w-3/5" },
  });
  const network = helper.accessor((row) => row.asName, {
    id: "network",
    header: "Network",
    enableSorting: true,
    cell: ({ row }) => <NetworkColumnCell row={row.original} />,
    meta: { className: "max-w-0 w-[20%] min-w-36" },
  });
  const country = helper.accessor((row) => countryName(row.country), {
    id: "country",
    header: "Country",
    enableSorting: true,
    cell: ({ row }) => <CountryColumnCell row={row.original} />,
    meta: { className: "whitespace-nowrap" },
  });
  const port = helper.accessor((row) => row.port, {
    id: "port",
    header: "Port",
    enableSorting: true,
    cell: ({ row }) => <PortCell row={row.original} />,
  });
  const machines = helper.accessor((row) => row.nodes, {
    id: "machines",
    header: "Machines",
    enableSorting: true,
    sortDescFirst: true,
    cell: ({ row }) => <span className="tabular-nums">{row.original.nodes}</span>,
    meta: { numeric: true, className: "text-kumo-subtle" },
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
        {...(row.original.of === undefined ? {} : { of: row.original.of })}
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
  const directions = widths.sm ? [upload, download] : [];

  if (groupBy === "destination") {
    return helper.columns([
      key,
      ...(widths.lg ? [network] : []),
      ...(widths.xl ? [country] : []),
      ...(widths.md ? [port] : []),
      volume,
      ...directions,
      ...(widths["2xl"] ? [conns] : []),
    ]);
  }

  // A row of machines or gateways is one machine, and so is every row of a view narrowed to one;
  // otherwise a row gathers several.
  const many = !oneMachine && groupBy !== "node" && groupBy !== "reporter" && widths.md;

  return helper.columns([
    key,
    ...(many ? [machines] : []),
    volume,
    ...directions,
    ...(widths.xl ? [conns] : []),
  ]);
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
  of,
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
  /** What `whole` is, for the share a total's tooltip gives; the window when absent. */
  readonly of?: string;
  readonly footer?: ReactNode;
  readonly empty?: ReactNode;
  /** Called with the clicked row; a folded remainder row is not clickable. */
  readonly onPick?: (row: TrafficDestination) => void;
}): ReactElement {
  const widths = useWidths();
  const widest = Math.max(0, ...rows.map((row) => total(row)));
  const data: Row[] = rows.map((row) => ({ ...row, widest, whole, of }));
  const byId = new Map(rows.map((row) => [destinationKey(row, groupBy), row]));
  const pickable = (id: string): TrafficDestination | undefined => {
    const row = byId.get(id);

    return row === undefined || isRemainder(row, groupBy) ? undefined : row;
  };
  const table = useAppTable({
    data,
    columns: columnsFor(groupBy, oneMachine, widths),
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
