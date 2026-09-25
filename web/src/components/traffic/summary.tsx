import type { TimeseriesData } from "@cloudflare/kumo/components/chart";
import { ChartLegend, ChartPalette, TimeseriesChart } from "@cloudflare/kumo/components/chart";
import { cn } from "@cloudflare/kumo/utils";
import { useSyncExternalStore } from "react";
import type { ReactElement } from "react";

import type { TrafficPoint, TrafficSummary } from "~/api/traffic.ts";
import { formatBytes, formatCount, formatRate } from "~/components/traffic/format.ts";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";
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

// Tailwind's `sm`: below it the chart is phone-wide and only has room for a few time labels.
const wideQuery = "(min-width: 40rem)";
const narrowTicks = 3;

function subscribeWide(onChange: () => void): () => void {
  const media = globalThis.matchMedia(wideQuery);

  media.addEventListener("change", onChange);

  return () => {
    media.removeEventListener("change", onChange);
  };
}

function useWide(): boolean {
  return useSyncExternalStore(subscribeWide, () => globalThis.matchMedia(wideQuery).matches);
}

function tickFormat(summary: TrafficSummary): (value: number) => string {
  const start = parseTime(summary.start)?.getTime() ?? 0;
  const end = parseTime(summary.end)?.getTime() ?? 0;

  return (value) => (end - start <= dayMs ? timeOfDay : dayOfMonth).format(new Date(value));
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
    <div className={cn(framePanelClass, "flex min-w-0 flex-col gap-1 px-5 py-4")}>
      <span className="text-sm text-kumo-subtle">{label}</span>
      <span className="text-xl font-semibold text-kumo-strong tabular-nums">{value}</span>
      <span className="truncate text-sm text-kumo-subtle">{hint}</span>
    </div>
  );
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
  const wide = useWide();
  const rates = ratesOf(summary.series, summary.resolution);
  const uploadColour = ChartPalette.categorical(0, dark);
  const downloadColour = ChartPalette.categorical(1, dark);
  const data: TimeseriesData[] = [
    { name: "Upload", color: uploadColour, data: rates.upload },
    { name: "Download", color: downloadColour, data: rates.download },
  ];
  const start = parseTime(summary.start);
  const end = parseTime(summary.end);

  return (
    <Frame className="grid gap-1 sm:grid-cols-3">
      <Stat
        label="Upload"
        value={formatBytes(summary.total.txBytes)}
        hint={`Peak ${formatRate(rates.peakUpload)}`}
      />
      <Stat
        label="Download"
        value={formatBytes(summary.total.rxBytes)}
        hint={`Peak ${formatRate(rates.peakDownload)}`}
      />
      <Stat
        label="Connections"
        value={formatCount(summary.total.conns)}
        hint={`${formatCount(summary.total.txPackets + summary.total.rxPackets)} packets`}
      />
      <div className={cn(framePanelClass, "flex min-w-0 flex-col gap-3 px-5 py-4 sm:col-span-3")}>
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <span className="text-sm text-kumo-subtle">
            Average rate
            {summary.resolution > 0 ? ` per ${resolutionLabel(summary.resolution)}` : ""}
          </span>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
            <ChartLegend.SmallItem
              name="Upload"
              color={uploadColour}
              value={formatBytes(summary.total.txBytes)}
            />
            <ChartLegend.SmallItem
              name="Download"
              color={downloadColour}
              value={formatBytes(summary.total.rxBytes)}
            />
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
          yAxisTickFormat={formatRate}
          tooltipValueFormat={formatRate}
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

const resolutionNames: ReadonlyMap<number, string> = new Map([
  [minuteSeconds, "minute"],
  [hourSeconds, "hour"],
  [daySeconds, "day"],
]);

function resolutionLabel(resolution: number): string {
  return resolutionNames.get(resolution) ?? `${resolution} seconds`;
}
