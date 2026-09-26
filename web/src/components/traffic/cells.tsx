import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import type { ReactElement } from "react";

import { formatBytes, shareLabel } from "~/components/traffic/format.ts";

const percent = 100;

/** How much of the bar a value fills against the largest one, at least a sliver when non-zero. */
export function barWidth(value: number, widest: number): number {
  if (widest <= 0 || value <= 0) {
    return 0;
  }

  return Math.max((value / widest) * percent, 2);
}

/**
 * Bytes with a bar behind them sized against the largest row, so the shape of who uses the gateways
 * reads at a glance. The share of the whole window is one hover away.
 */
export function VolumeCell({
  bytes,
  widest,
  whole,
  of = "the window",
}: {
  readonly bytes: number;
  /** The largest value in the column, which fills the bar. */
  readonly widest: number;
  /** Everything in the window, for the share. */
  readonly whole: number;
  /** What `whole` is, in the tooltip's words. */
  readonly of?: string;
}): ReactElement {
  return (
    <span className="flex items-center justify-end gap-2">
      <Tooltip content={`${shareLabel(bytes, whole)} of ${of}`}>
        <span className="whitespace-nowrap tabular-nums">{formatBytes(bytes)}</span>
      </Tooltip>
      <span aria-hidden className="h-1.5 w-20 shrink-0 overflow-clip rounded-full bg-kumo-tint">
        <span
          className="block h-full rounded-full bg-kumo-info"
          style={{ width: `${barWidth(bytes, widest)}%` }}
        />
      </span>
    </span>
  );
}

/** Plain bytes, right aligned by the column. */
export function BytesCell({ bytes }: { readonly bytes: number }): ReactElement {
  return <span className="whitespace-nowrap tabular-nums">{formatBytes(bytes)}</span>;
}
