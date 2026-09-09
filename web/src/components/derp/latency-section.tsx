import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import { InfoIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { DerpLatencyRegion, DerpLatencyReport } from "~/api/schema.gen.ts";
import {
  barPercent,
  linkLabel,
  machineRows,
  maxPreferredBy,
  msLabel,
  msValue,
  rangeValue,
} from "~/components/derp/latency-model.ts";
import type { LatencyMachineRow } from "~/components/derp/latency-model.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { Dot, Status } from "~/components/ui/status.tsx";

const infoSize = 14;

/** The relay latency report as a page: what the tailnet measured and who is farthest from home. */
export function LatencySection({ report }: { readonly report: DerpLatencyReport }): ReactElement {
  return (
    <div className="flex flex-col gap-6">
      <StatStrip report={report} />
      <RegionsSection regions={report.regions} />
      <MachinesSection report={report} />
    </div>
  );
}

function StatStrip({ report }: { readonly report: DerpLatencyReport }): ReactElement {
  return (
    <Frame className="grid gap-1 sm:grid-cols-3">
      <Stat label="Reporting" value={report.reporting} hint="Machines that measured the relays" />
      <Stat label="Silent" value={report.silent} hint="Machines that measured none" />
      <Stat
        label="Behind a hard NAT"
        value={report.hardNat}
        hint="They relay most of their traffic"
        explain="A hard NAT gives every destination a different port, so two machines behind one cannot guess where to send the first packet and fall back to a relay."
      />
    </Frame>
  );
}

function Stat({
  label,
  value,
  hint,
  explain,
}: {
  readonly label: string;
  readonly value: number;
  readonly hint: string;
  /** A sentence the label alone cannot carry, one hover or tap away. */
  readonly explain?: string;
}): ReactElement {
  return (
    <div className={cn(framePanelClass, "flex flex-col gap-1 px-5 py-4")}>
      <span className="flex items-center gap-1 text-sm text-kumo-subtle">
        {label}
        {explain === undefined ? null : <Explain hint={explain} />}
      </span>
      <span className="text-xl font-semibold text-kumo-strong tabular-nums">{value}</span>
      <span className="text-sm text-kumo-subtle">{hint}</span>
    </div>
  );
}

/** The trigger is a Kumo button, so the keyboard reaches the sentence and it has a name. */
function Explain({ hint }: { readonly hint: string }): ReactElement {
  return (
    <Tooltip
      content={hint}
      render={
        <Button
          variant="ghost"
          shape="square"
          size="xs"
          aria-label={hint}
          className="h-lh text-kumo-subtle"
          icon={<InfoIcon size={infoSize} aria-hidden />}
        />
      }
    />
  );
}

const regionHelper = createAppColumnHelper<DerpLatencyRegion>();

const regionColumns = regionHelper.columns([
  regionHelper.display({
    id: "region",
    header: "Region",
    cell: ({ row }) => <RegionCell region={row.original} />,
  }),
  regionHelper.display({
    id: "preferredBy",
    header: "Preferred by",
    // The widest count comes off the whole collection, not the page, so a bar means the same thing
    // on every page of the table.
    cell: ({ row, table }) => (
      <PreferredCell count={row.original.preferredBy} widest={maxPreferredBy(table.options.data)} />
    ),
    meta: { numeric: true },
  }),
  regionHelper.display({
    id: "samples",
    header: "Measured by",
    cell: ({ row }) => row.original.samples,
    meta: { numeric: true, className: "text-kumo-subtle" },
  }),
  regionHelper.display({
    id: "median",
    header: "Median (ms)",
    cell: ({ row }) => msValue(row.original.medianMs),
    meta: { numeric: true },
  }),
  regionHelper.display({
    id: "p90",
    header: "p90 (ms)",
    cell: ({ row }) => msValue(row.original.p90Ms),
    meta: { numeric: true },
  }),
  regionHelper.display({
    id: "range",
    header: "Min / max (ms)",
    cell: ({ row }) => rangeValue(row.original),
    meta: { numeric: true, className: "text-kumo-subtle" },
  }),
]);

function RegionsSection({
  regions,
}: {
  readonly regions: readonly DerpLatencyRegion[];
}): ReactElement {
  const table = useAppTable({
    data: regions,
    columns: regionColumns,
    getRowId: (region) => String(region.regionId),
  });

  return (
    <Section
      title="Regions"
      description="What the machines measured, region by region. A round trip is the best of the client's recent probes."
      panel={false}
    >
      <table.AppTable>
        <DataTable
          empty={
            <SectionEmpty
              title="No measurements yet"
              description="Machines report their relay latency once they connect."
            />
          }
          footer={
            regions.length === 0 ? undefined : (
              <TableFooter>{`Showing ${plural(regions.length, "region")}`}</TableFooter>
            )
          }
        />
      </table.AppTable>
    </Section>
  );
}

function RegionCell({ region }: { readonly region: DerpLatencyRegion }): ReactElement {
  return (
    <span className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
      <span className="font-mono">{region.code}</span>
      <span className="truncate text-kumo-subtle">{region.name}</span>
      {region.inMap ? null : <Status tone="warning">Removed</Status>}
    </span>
  );
}

/** The count with a bar behind it, so the shape of the tailnet's homes reads at a glance. */
function PreferredCell({
  count,
  widest,
}: {
  readonly count: number;
  readonly widest: number;
}): ReactElement {
  return (
    <span className="flex items-center justify-end gap-2">
      <span className="tabular-nums">{count}</span>
      <span aria-hidden className="h-1.5 w-16 shrink-0 overflow-clip rounded-full bg-kumo-tint">
        <span
          className="block h-full rounded-full bg-kumo-info"
          style={{ width: `${barPercent(count, widest)}%` }}
        />
      </span>
    </span>
  );
}

const machineHelper = createAppColumnHelper<LatencyMachineRow>();

const machineColumns = machineHelper.columns([
  machineHelper.display({
    id: "machine",
    header: "Machine",
    cell: ({ row }) => <MachineCell machine={row.original.machine} />,
  }),
  machineHelper.display({
    id: "home",
    header: "Home region",
    cell: ({ row }) => <HomeCell home={row.original.home} />,
  }),
  machineHelper.display({
    id: "roundTrip",
    header: "Round trip",
    cell: ({ row }) => msLabel(row.original.machine.homeMs),
    meta: { numeric: true },
  }),
  machineHelper.display({
    id: "link",
    header: "Link",
    cell: ({ row }) => linkLabel(row.original.machine.linkType),
    meta: { className: "text-kumo-subtle" },
  }),
  machineHelper.display({
    id: "nat",
    header: "NAT",
    cell: ({ row }) => (
      <Status tone={row.original.machine.hardNat ? "warning" : "success"}>
        {row.original.machine.hardNat ? "Hard" : "Easy"}
      </Status>
    ),
  }),
]);

function MachinesSection({ report }: { readonly report: DerpLatencyReport }): ReactElement {
  const rows = machineRows(report);
  const table = useAppTable({
    data: rows,
    columns: machineColumns,
    getRowId: (row) => row.machine.nodeId,
  });

  return (
    <Section
      title="Farthest from home"
      description="Machines whose round trip to their own relay region is the worst, first."
      panel={false}
    >
      <table.AppTable>
        <DataTable
          empty={
            <SectionEmpty
              title="No machines reporting"
              description="A machine appears here once its client sends a network report."
            />
          }
          footer={
            rows.length === 0 ? undefined : (
              <TableFooter>{`Showing ${plural(rows.length, "machine")}`}</TableFooter>
            )
          }
        />
      </table.AppTable>
    </Section>
  );
}

function MachineCell({
  machine,
}: {
  readonly machine: LatencyMachineRow["machine"];
}): ReactElement {
  return (
    <Link
      to="/machines/$nodeId"
      params={{ nodeId: machine.nodeId }}
      className="flex items-center gap-2 hover:underline"
    >
      <Dot tone={machine.online ? "success" : "neutral"} />
      <span className="sr-only">{machine.online ? "Connected" : "Disconnected"}</span>
      <span className="truncate">{machine.name}</span>
    </Link>
  );
}

function HomeCell({ home }: { readonly home: DerpLatencyRegion | undefined }): ReactElement {
  if (home === undefined) {
    return <span className="text-kumo-subtle">Unknown</span>;
  }

  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">{home.code}</span>
      <span className="truncate text-kumo-subtle">{home.name}</span>
    </span>
  );
}
