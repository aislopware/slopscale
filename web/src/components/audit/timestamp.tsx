import type { ReactElement } from "react";

import { formatAbsolute, formatRelative, parseTime } from "~/lib/time.ts";

const pad = 2;

function twoDigits(value: number): string {
  return String(value).padStart(pad, "0");
}

/** The local date as 2026-10-20: sortable, and the same width on every row. */
function isoDate(date: Date): string {
  return `${date.getFullYear()}-${twoDigits(date.getMonth() + 1)}-${twoDigits(date.getDate())}`;
}

/** The local time of day as 16:02:27. */
function isoTime(date: Date): string {
  return `${twoDigits(date.getHours())}:${twoDigits(date.getMinutes())}:${twoDigits(date.getSeconds())}`;
}

/**
 * A moment in a log table, as the dashboard's audit log writes it: the date over the time, in Paper
 * Mono with tabular figures so the rows line up, the time stepped back. How long ago it was is on
 * hover, with the full date.
 */
export function Timestamp({ value }: { readonly value: string | null | undefined }): ReactElement {
  const at = parseTime(value);

  if (at === null) {
    return <span className="text-kumo-subtle">{value ?? ""}</span>;
  }

  return (
    <time
      dateTime={at.toISOString()}
      title={`${formatAbsolute(at)} (${formatRelative(at)})`}
      className="flex flex-col gap-0.5 font-mono tabular-nums"
    >
      <span className="text-sm text-kumo-default">{isoDate(at)}</span>
      <span className="text-xs text-kumo-subtle">{isoTime(at)}</span>
    </time>
  );
}
