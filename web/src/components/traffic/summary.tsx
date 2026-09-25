import type { TimeseriesData } from "@cloudflare/kumo/components/chart";
import { ChartLegend, ChartPalette, TimeseriesChart } from "@cloudflare/kumo/components/chart";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import type { TrafficPoint, TrafficSummary } from "~/api/traffic.ts";
import {
  formatBytes,
  formatCount,
  formatRate,
  formatScaled,
  rateScale,
} from "~/components/traffic/format.ts";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";
import { useMinWidth } from "~/lib/breakpoint.ts";
import { echarts } from "~/lib/echarts.ts";
import { useDarkMode } from "~/lib/theme.ts";
import { daySeconds, formatAbsolute, hourSeconds, minuteSeconds, parseTime } from "~/lib/time.ts";

const millisecond = 1000;
const chartHeight = 220;
const dayMs = daySeconds * millisecond;
const timeOfDay = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const dayOfMonth = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

export interface Rates {
  /** Average upload rate per bucket, as `[time, bytes per second]`. */
  readonly upload: [number, number][];
  readonly download: [number, number][];
  /** The busiest bucket's rate in each direction. */
  readonly peakUpload: number;
  readonly peakDownload: number;
}

/**
 * The series as average rates: a bucket's bytes over its width. A rate reads the same whether the
 * window is an hour of minutes or a quarter of days, where raw per-bucket bytes would jump by the
 * bucket width when the resolution changes.
 */
export function ratesOf(series: readonly TrafficPoint[], resolution: number): Rates {
  const width = resolution > 0 ? resolution : 1;
  const upload: [number, number][] = [];
  const download: [number, number][] = [];
  let peakUpload = 0;
  let peakDownload = 0;

  for (const point of series) {
    const time = parseTime(point.start)?.getTime();

    if (time !== undefined) {
      const up = point.txBytes / width;
      const down = point.rxBytes / width;

      upload.push([time, up]);
      download.push([time, down]);
      peakUpload = Math.max(peakUpload, up);
      peakDownload = Math.max(peakDownload, down);
    }
  }

  return { upload, download, peakUpload, peakDownload };
}

/** What the page draws before the first answer arrives. */
export const emptySummary: TrafficSummary = {
  start: "",
  end: "",
  resolution: 0,
  total: { conns: 0, rxBytes: 0, rxPackets: 0, txBytes: 0, txPackets: 0 },
  series: [],
  nodes: [],
  reporters: [],
};

// Below `sm` the chart is phone-wide and only has room for a few time labels.
const narrowTicks = 3;

function tickFormat(summary: TrafficSummary): (value: number) => string {
  const start = parseTime(summary.start)?.getTime() ?? 0;
  const end = parseTime(summary.end)?.getTime() ?? 0;

  return (value) => (end - start <= dayMs ? timeOfDay : dayOfMonth).format(new Date(value));
}

function scaledBy(points: readonly [number, number][], divisor: number): [number, number][] {
  return points.map(([time, rate]) => [time, rate / divisor]);
}

function Stat({
  label,
  value,
  hint,
}: {
  readonly label: string;
  readonly value: string;
  readonly hint: string;
}): ReactElement {
  return (
    <div
      className={cn(
        framePanelClass,
        "flex min-w-0 flex-col gap-0.5 px-3 py-3 sm:gap-1 sm:px-5 sm:py-4",
      )}
    >
      <span className="text-xs text-kumo-subtle sm:text-sm">{label}</span>
      <span className="text-base font-semibold text-kumo-strong tabular-nums sm:text-xl">
        {value}
      </span>
      <span className="text-xs text-pretty text-kumo-subtle sm:truncate sm:text-sm" title={hint}>
        {hint}
      </span>
    </div>
  );
}

const resolutionNames: ReadonlyMap<number, string> = new Map([
  [minuteSeconds, "minute"],
  [hourSeconds, "hour"],
  [daySeconds, "day"],
]);

/** "Busiest hour 6.9 MiB/s": the peak, named by the stretch it averages over. */
function busiest(resolution: number, rate: number): string {
  const name = resolutionNames.get(resolution);

  return name === undefined ? `Busiest ${formatRate(rate)}` : `Busiest ${name} ${formatRate(rate)}`;
}

/** ", 1-minute buckets": what one point on the chart averages over. */
function bucketLabel(resolution: number): string {
  if (resolution <= 0) {
    return "";
  }

  const name = resolutionNames.get(resolution);

  return name === undefined ? `, ${resolution}-second buckets` : `, 1-${name} buckets`;
}

/**
 * The window at a glance: what left the tailnet through its gateways, what came back, how many
 * connections that took, and the rate over time. Dragging across the chart asks for that stretch as
 * a custom window.
 */
export function TrafficSummaryPanel({
  summary,
  loading = false,
  onZoom,
}: {
  readonly summary: TrafficSummary;
  readonly loading?: boolean;
  /** Called with the dragged-over stretch, as RFC 3339 bounds. */
  readonly onZoom?: (start: string, end: string) => void;
}): ReactElement {
  const dark = useDarkMode();
  const wide = useMinWidth("sm");
  const rates = ratesOf(summary.series, summary.resolution);
  const scale = rateScale(Math.max(rates.peakUpload, rates.peakDownload));
  const uploadColour = ChartPalette.categorical(0, dark);
  const downloadColour = ChartPalette.categorical(1, dark);
  const data: TimeseriesData[] = [
    { name: "Upload", color: uploadColour, data: scaledBy(rates.upload, scale.divisor) },
    { name: "Download", color: downloadColour, data: scaledBy(rates.download, scale.divisor) },
  ];
  const start = parseTime(summary.start);
  const end = parseTime(summary.end);

  return (
    <Frame className="grid grid-cols-3 gap-1">
      <Stat
        label="Upload"
        value={formatBytes(summary.total.txBytes)}
        hint={busiest(summary.resolution, rates.peakUpload)}
      />
      <Stat
        label="Download"
        value={formatBytes(summary.total.rxBytes)}
        hint={busiest(summary.resolution, rates.peakDownload)}
      />
      <Stat
        label="Connections"
        value={formatCount(summary.total.conns)}
        hint={`${formatCount(summary.total.txPackets + summary.total.rxPackets)} packets`}
      />
      <div className={cn(framePanelClass, "col-span-3 flex min-w-0 flex-col gap-3 px-5 py-4")}>
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <span className="text-sm text-kumo-subtle">
            {`Average rate${bucketLabel(summary.resolution)}`}
          </span>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <ChartLegend.SmallItem name="Upload" color={uploadColour} value="" />
            <ChartLegend.SmallItem name="Download" color={downloadColour} value="" />
          </div>
        </div>
        <TimeseriesChart
          echarts={echarts}
          type="line"
          gradient
          data={data}
          height={chartHeight}
          isDarkMode={dark}
          loading={loading}
          yAxisTickCount={4}
          {...(wide ? {} : { xAxisTickCount: narrowTicks })}
          yAxisTickFormat={(value) => formatScaled(value, scale)}
          tooltipValueFormat={(value) => formatRate(value * scale.divisor)}
          tooltipFollowCursor="x"
          xAxisTickFormat={tickFormat(summary)}
          {...(onZoom === undefined
            ? {}
            : {
                onTimeRangeChange: (from: number, to: number) => {
                  onZoom(new Date(from).toISOString(), new Date(to).toISOString());
                },
              })}
          ariaDescription={
            start === null || end === null
              ? "Upload and download rate"
              : `Upload and download rate from ${formatAbsolute(start)} to ${formatAbsolute(end)}`
          }
        />
      </div>
    </Frame>
  );
}
