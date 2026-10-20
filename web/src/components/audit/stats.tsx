import type { TimeseriesData } from "@cloudflare/kumo/components/chart";
import { ChartPalette, TimeseriesChart } from "@cloudflare/kumo/components/chart";
import type { ReactElement } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { clientError } from "~/components/audit/cells.tsx";
import { Section } from "~/components/ui/section.tsx";
import { StatGrid, StatTile } from "~/components/ui/stat-tile.tsx";
import { echarts } from "~/lib/echarts.ts";
import { useDarkMode } from "~/lib/theme.ts";
import { daySeconds, formatAbsolute, minuteSeconds, parseTime } from "~/lib/time.ts";

/** One actor: the user behind the call, or the credential kind when no user is bound. */
function actorKey(event: AuditEvent): string {
  return event.actorUserId === "" ? `${event.actorKind}:${event.actorName}` : event.actorUserId;
}

/** How many bars the strip has; each is the same slice of the loaded window. */
export const bucketCount = 24;
const millisecond = 1000;
/** The narrowest window a bar can stand for, so a burst within one minute still spreads out. */
const minSpanMs = bucketCount * minuteSeconds * millisecond;

export interface Bucket {
  readonly start: Date;
  readonly total: number;
  readonly failed: number;
}

/**
 * The loaded events split into equal slices from the oldest one to now. The log is server-paged, so
 * the strip covers what is on screen; loading more reaches further back.
 */
export function bucketEvents(events: readonly AuditEvent[], now = new Date()): Bucket[] {
  const times = events.map((event) => parseTime(event.createdAt)?.getTime() ?? Number.NaN);
  const oldest = Math.min(...times.filter((time) => !Number.isNaN(time)));

  if (!Number.isFinite(oldest)) {
    return [];
  }

  const end = now.getTime();
  const span = Math.max(end - oldest, minSpanMs);
  const width = span / bucketCount;
  const counts = Array.from({ length: bucketCount }, (_, index) => ({
    start: new Date(end - span + index * width),
    total: 0,
    failed: 0,
  }));

  events.forEach((event, index) => {
    const time = times[index];

    if (time === undefined || Number.isNaN(time)) {
      return;
    }

    const slot = Math.min(Math.floor((time - (end - span)) / width), bucketCount - 1);
    const bucket = counts[Math.max(slot, 0)];

    if (bucket !== undefined) {
      bucket.total += 1;
      bucket.failed += event.outcome >= clientError ? 1 : 0;
    }
  });

  return counts;
}

const chartHeight = 140;
const dayMs = daySeconds * millisecond;
const timeOfDay = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const dayOfMonth = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

/** A legend entry on the chart card's band: the series' dot, its total and its name. */
function LegendItem({
  color,
  value,
  label,
}: {
  readonly color: string;
  readonly value: number;
  readonly label: string;
}): ReactElement {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm text-kumo-subtle">
      <span aria-hidden className="size-2.5 rounded-full" style={{ backgroundColor: color }} />
      <span className="tabular-nums">{value.toLocaleString()}</span>
      {label}
    </span>
  );
}

/**
 * Event volume over the loaded window, as the dashboard's "Actions over time" card: one stacked bar
 * per slice with the failed share in the attention colour, so it shows where the activity was and
 * whether the failures came in a burst or trickled through, with the two totals as the legend on
 * the card's band. Kumo's chart draws the axes and tooltip; the palette follows the resolved
 * theme.
 */
function Activity({
  events,
  failures,
}: {
  readonly events: readonly AuditEvent[];
  readonly failures: number;
}): ReactElement | null {
  const dark = useDarkMode();
  const buckets = bucketEvents(events);
  const tallest = Math.max(...buckets.map((bucket) => bucket.total));
  const [first] = buckets;

  if (first === undefined || tallest === 0) {
    return null;
  }

  const from = first.start;
  const last = buckets.at(-1)?.start ?? from;
  const withinDay = last.getTime() - from.getTime() < dayMs;
  const succeeded = ChartPalette.categorical(0, dark);
  const failed = ChartPalette.semantic("Attention", dark);
  const series: TimeseriesData[] = [
    {
      name: "Succeeded",
      color: succeeded,
      data: buckets.map((bucket) => [bucket.start.getTime(), bucket.total - bucket.failed]),
    },
    {
      name: "Failed",
      color: failed,
      data: buckets.map((bucket) => [bucket.start.getTime(), bucket.failed]),
    },
  ];

  return (
    <Section
      title="Actions over time"
      actions={
        <span className="flex flex-wrap items-center gap-4">
          <LegendItem color={succeeded} value={events.length - failures} label="Success" />
          <LegendItem color={failed} value={failures} label="Failed" />
        </span>
      }
      bodyClassName="px-4 py-3"
    >
      <TimeseriesChart
        echarts={echarts}
        type="bar"
        data={series}
        height={chartHeight}
        isDarkMode={dark}
        yAxisTickCount={3}
        yAxisMinInterval={1}
        xAxisTickFormat={(value) => (withinDay ? timeOfDay : dayOfMonth).format(new Date(value))}
        tooltipValueFormat={(value) => `${value} ${value === 1 ? "event" : "events"}`}
        ariaDescription={`Events over time, ${bucketCount} bars from ${formatAbsolute(from)} to now`}
      />
    </Section>
  );
}

const percent = 100;

/**
 * What the loaded pages add up to: the activity chart, then the totals as the dashboard's stat
 * tiles. The server pages the log, so these count what is on screen, which is what an operator is
 * reading through; loading more raises them.
 */
export function AuditStats({ events }: { readonly events: readonly AuditEvent[] }): ReactElement {
  const actors = new Set(events.map((event) => actorKey(event)));
  const failures = events.filter((event) => event.outcome >= clientError).length;
  const rate = events.length === 0 ? 0 : Math.round((failures / events.length) * percent);

  return (
    <div className="flex flex-col gap-4">
      <Activity events={events} failures={failures} />
      <StatGrid className="sm:grid-cols-3">
        <StatTile label="Events loaded" value={events.length} hint="In the selected range" />
        <StatTile
          label="Distinct actors"
          value={actors.size}
          hint="Users, machines, keys and sessions"
        />
        <StatTile
          label="Failures"
          value={failures}
          hint={`${rate}% of loaded events, HTTP status 400 or higher`}
          tone={failures > 0 ? "danger" : "default"}
        />
      </StatGrid>
    </div>
  );
}
