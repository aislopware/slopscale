import type { TimeseriesData } from "@cloudflare/kumo/components/chart";
import { ChartPalette, TimeseriesChart } from "@cloudflare/kumo/components/chart";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { clientError } from "~/components/audit/cells.tsx";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";
import { echarts } from "~/lib/echarts.ts";
import { useDarkMode } from "~/lib/theme.ts";
import { daySeconds, formatAbsolute, minuteSeconds, parseTime } from "~/lib/time.ts";

/** One actor: the user behind the call, or the credential kind when no user is bound. */
function actorKey(event: AuditEvent): string {
  return event.actorUserId === "" ? `${event.actorKind}:${event.actorName}` : event.actorUserId;
}

function Stat({
  label,
  value,
  hint,
  alert = false,
}: {
  readonly label: string;
  readonly value: number;
  readonly hint: string;
  /** Draws the number in the danger colour once it is not zero. */
  readonly alert?: boolean;
}): ReactElement {
  return (
    <div className={cn(framePanelClass, "flex flex-col gap-1 px-5 py-4")}>
      <span className="text-sm text-kumo-subtle">{label}</span>
      <span
        className={cn(
          "text-xl font-semibold tabular-nums",
          alert && value > 0 ? "text-kumo-danger" : "text-kumo-strong",
        )}
      >
        {value}
      </span>
      <span className="text-sm text-kumo-subtle">{hint}</span>
    </div>
  );
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

/**
 * Event volume over the loaded window, one stacked bar per slice with the failed share in the
 * attention colour: where the activity was, and whether the failures came in a burst or trickled
 * through. Kumo's chart draws the axes and tooltip; the palette follows the resolved theme.
 */
function Activity({ events }: { readonly events: readonly AuditEvent[] }): ReactElement | null {
  const dark = useDarkMode();
  const buckets = bucketEvents(events);
  const tallest = Math.max(...buckets.map((bucket) => bucket.total));

  if (buckets.length === 0 || tallest === 0) {
    return null;
  }

  const from = buckets[0]?.start ?? new Date();
  const last = buckets.at(-1)?.start ?? from;
  const withinDay = last.getTime() - from.getTime() < dayMs;
  const series: TimeseriesData[] = [
    {
      name: "Succeeded",
      color: ChartPalette.categorical(0, dark),
      data: buckets.map((bucket) => [bucket.start.getTime(), bucket.total - bucket.failed]),
    },
    {
      name: "Failed",
      color: ChartPalette.semantic("Attention", dark),
      data: buckets.map((bucket) => [bucket.start.getTime(), bucket.failed]),
    },
  ];

  return (
    <div className={cn(framePanelClass, "flex flex-col gap-1 px-5 py-4 sm:col-span-3")}>
      <span className="text-sm text-kumo-subtle">Activity</span>
      <TimeseriesChart
        echarts={echarts}
        type="bar"
        data={series}
        height={chartHeight}
        isDarkMode={dark}
        yAxisTickCount={3}
        xAxisTickFormat={(value) => (withinDay ? timeOfDay : dayOfMonth).format(new Date(value))}
        tooltipValueFormat={(value) => `${value} ${value === 1 ? "event" : "events"}`}
        ariaDescription={`Events over time, ${bucketCount} bars from ${formatAbsolute(from)} to now`}
      />
    </div>
  );
}

/**
 * What the loaded pages add up to. The server pages the log, so these count what is on screen,
 * which is what an operator is reading through; loading more raises them.
 */
export function AuditStats({ events }: { readonly events: readonly AuditEvent[] }): ReactElement {
  const actors = new Set(events.map((event) => actorKey(event)));
  const failures = events.filter((event) => event.outcome >= clientError).length;

  return (
    <Frame className="grid gap-1 sm:grid-cols-3">
      <Stat label="Events loaded" value={events.length} hint="In the selected range" />
      <Stat label="Distinct actors" value={actors.size} hint="Users, machines, keys and sessions" />
      <Stat label="Failures" value={failures} hint="HTTP status 400 or higher" alert />
      <Activity events={events} />
    </Frame>
  );
}
