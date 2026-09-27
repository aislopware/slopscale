import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { cn } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import type { TrafficDestination, TrafficNode, TrafficSummary } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { VolumeCell } from "~/components/traffic/cells.tsx";
import { HostCell, isRemainder } from "~/components/traffic/destinations-table.tsx";
import { trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { defaultTrafficRange } from "~/components/traffic/range.ts";
import { noDestinationFilters, pickDestination } from "~/components/traffic/search.ts";
import { TrafficSummaryPanels, emptySummary } from "~/components/traffic/summary.tsx";
import { textLinkClass } from "~/components/traffic/window-header.tsx";
import { framePanelClass } from "~/components/ui/frame.tsx";
import { MachineName } from "~/components/ui/machine-name.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";

/** The window the overview reads: the traffic pages' default, so their links open the same one. */
export const glanceWindow = { range: defaultTrafficRange, from: "", to: "", gateway: "" } as const;

/** Rows each list shows; the traffic pages list the rest. */
export const glanceRows = 5;

const rowClass =
  "flex items-center gap-4 px-5 py-2.5 text-kumo-default no-underline not-first:border-t not-first:border-kumo-hairline";

function total(row: { readonly txBytes: number; readonly rxBytes: number }): number {
  return row.txBytes + row.rxBytes;
}

/** A list panel beside its twin: a label, the rows, and where the rest of them are. */
function ListPanel({
  label,
  more,
  loading,
  empty,
  children,
}: {
  readonly label: string;
  readonly more: ReactNode;
  readonly loading: boolean;
  readonly empty: boolean;
  readonly children: ReactNode;
}): ReactElement {
  let body = children;

  if (loading) {
    body = Array.from({ length: glanceRows }, (_, index) => (
      <div key={index} aria-busy className={rowClass}>
        <SkeletonLine blockHeight={20} minWidth={30} maxWidth={60} />
      </div>
    ));
  } else if (empty) {
    body = <SectionEmpty title="Nothing in the last 24 hours" />;
  }

  return (
    <div className={cn(framePanelClass, "flex min-w-0 flex-col overflow-clip")}>
      <div className="flex items-center justify-between gap-4 px-5 pt-4 pb-2">
        <span className="text-sm text-kumo-subtle">{label}</span>
        {more}
      </div>
      <div className="flex flex-col">{body}</div>
    </div>
  );
}

function DestinationRow({
  row,
  widest,
  whole,
}: {
  readonly row: TrafficDestination;
  readonly widest: number;
  readonly whole: number;
}): ReactElement {
  const content = (
    <>
      <span className="flex min-w-0 flex-1 flex-col items-start gap-0.5">
        <HostCell row={row} />
        <span className="text-xs text-kumo-subtle">{plural(row.nodes, "machine")}</span>
      </span>
      <VolumeCell bytes={total(row)} widest={widest} whole={whole} of="the last 24 hours" />
    </>
  );

  // The folded remainder stands for no one host, so there is nothing for it to open.
  if (isRemainder(row, "host")) {
    return <div className={rowClass}>{content}</div>;
  }

  return (
    <Link
      to="/traffic/destinations"
      search={{
        ...glanceWindow,
        ...noDestinationFilters,
        by: "node",
        ...pickDestination(row, "host"),
      }}
      className={cn(rowClass, "hover:bg-kumo-tint")}
    >
      {content}
    </Link>
  );
}

function MachineRow({
  node,
  widest,
  whole,
}: {
  readonly node: TrafficNode;
  readonly widest: number;
  readonly whole: number;
}): ReactElement {
  return (
    <Link
      to="/traffic/machines/$nodeId"
      params={{ nodeId: node.nodeId }}
      search={{ ...glanceWindow, by: "host" }}
      className={cn(rowClass, "hover:bg-kumo-tint")}
    >
      <MachineName
        className="flex-1"
        name={
          <span
            className={cn(
              "max-w-full truncate",
              node.nodeName === "" ? "text-kumo-subtle" : "font-medium",
            )}
          >
            {trafficNodeName(node)}
          </span>
        }
        owner={node.nodeOwner}
      />
      <VolumeCell bytes={total(node)} widest={widest} whole={whole} of="the last 24 hours" />
    </Link>
  );
}

function widestOf(rows: readonly { readonly txBytes: number; readonly rxBytes: number }[]): number {
  return Math.max(0, ...rows.map((row) => total(row)));
}

export interface TrafficGlanceProps {
  /** Undefined until the first answer. */
  readonly summary: TrafficSummary | undefined;
  readonly destinations: readonly TrafficDestination[] | undefined;
  /** Why either read failed, or null. */
  readonly error: Error | null;
  /** Called with a stretch dragged across the chart, as RFC 3339 bounds. */
  readonly onZoom: (start: string, end: string) => void;
}

/**
 * The last day of gateway traffic on the overview: the totals and rate the traffic overview opens
 * with, then where it went and which machines sent it, each row opening its page there.
 */
export function TrafficGlance({
  summary,
  destinations,
  error,
  onZoom,
}: TrafficGlanceProps): ReactElement {
  const whole = summary === undefined ? 0 : total(summary.total);
  const machines = (summary?.nodes ?? []).slice(0, glanceRows);
  const hosts = (destinations ?? []).slice(0, glanceRows);
  const header = {
    title: "Traffic",
    description: "What the machines sent through the gateways in the last 24 hours.",
    actions: (
      <Link to="/traffic/overview" search={glanceWindow} className="text-kumo-link hover:underline">
        View traffic
      </Link>
    ),
  };

  if (error !== null) {
    return (
      <Section {...header} bodyClassName="p-0">
        <SectionEmpty title="Traffic could not be read" description={errorMessage(error)} />
      </Section>
    );
  }

  return (
    <Section {...header} panel={false}>
      <div className="flex flex-col gap-1">
        <div className="grid grid-cols-3 gap-1">
          <TrafficSummaryPanels
            summary={summary ?? emptySummary}
            loading={summary === undefined}
            onZoom={onZoom}
          />
        </div>
        <div className="grid gap-1 lg:grid-cols-2">
          <ListPanel
            label="Top destinations"
            loading={destinations === undefined}
            empty={hosts.length === 0}
            more={
              <Link
                to="/traffic/destinations"
                search={{ ...glanceWindow, ...noDestinationFilters, by: "host" }}
                className={textLinkClass}
              >
                All destinations
              </Link>
            }
          >
            {hosts.map((row) => (
              <DestinationRow key={row.host} row={row} widest={widestOf(hosts)} whole={whole} />
            ))}
          </ListPanel>
          <ListPanel
            label="Top machines"
            loading={summary === undefined}
            empty={machines.length === 0}
            more={
              <Link to="/traffic/machines" search={glanceWindow} className={textLinkClass}>
                All machines
              </Link>
            }
          >
            {machines.map((node) => (
              <MachineRow key={node.nodeId} node={node} widest={widestOf(machines)} whole={whole} />
            ))}
          </ListPanel>
        </div>
      </div>
    </Section>
  );
}
