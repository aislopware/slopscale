import { fallback, optional, picklist, pipe, transform, unknown } from "valibot";

import { parseTime } from "~/lib/time.ts";

/** The windows the traffic pages offer; "custom" reads its bounds from the address. */
export const trafficRanges = ["1h", "24h", "7d", "30d", "90d", "custom"] as const;

export type TrafficRange = (typeof trafficRanges)[number];

export const defaultTrafficRange = "24h" satisfies TrafficRange;

const minute = 60_000;
const hour = 3_600_000;
const day = 86_400_000;
const week = 7;
const month = 30;
const quarter = 90;

/** How far back each preset reaches; the longest stays inside what the server reads at once. */
const presetMs: Record<Exclude<TrafficRange, "custom">, number> = {
  "1h": hour,
  "24h": day,
  "7d": week * day,
  "30d": month * day,
  "90d": quarter * day,
};

/** The part of a traffic page's address that picks what it reads. */
export interface TrafficWindowSearch {
  readonly range: TrafficRange;
  /** RFC 3339 bounds of a custom range; empty otherwise. */
  readonly from: string;
  readonly to: string;
  /** A gateway (reporter node id) to keep, or "" for all of them. */
  readonly gateway: string;
}

export interface TrafficWindow {
  readonly start: string;
  readonly end: string;
}

/**
 * The bounds to ask for. A preset ends at the start of the current minute, so every read inside one
 * minute asks for the same window and hits the same cache entry. A custom range whose bounds do not
 * parse, or run backwards, falls back to the default preset rather than to an error page.
 */
export function trafficWindow(search: TrafficWindowSearch, now = new Date()): TrafficWindow {
  if (search.range === "custom") {
    const from = parseTime(search.from);
    const to = parseTime(search.to);

    if (from !== null && to !== null && from.getTime() < to.getTime()) {
      return { start: from.toISOString(), end: to.toISOString() };
    }
  }

  const preset = search.range === "custom" ? defaultTrafficRange : search.range;
  const end = Math.floor(now.getTime() / minute) * minute;

  return {
    start: new Date(end - presetMs[preset]).toISOString(),
    end: new Date(end).toISOString(),
  };
}

/** Whether the window keeps moving with the clock, so its data is worth refetching. */
export function isLive(search: TrafficWindowSearch): boolean {
  return search.range !== "custom";
}

/**
 * The router parses a search value as JSON, so `?gateway=2` arrives as the number 2 while the id it
 * names is a string. Reading it through this keeps the digits instead of dropping the filter.
 */
function toText(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }

  return typeof value === "number" ? String(value) : "";
}

const textValue = pipe(unknown(), transform(toText));

export const optionalText = fallback(optional(textValue, ""), "");

const rangeValue = picklist(trafficRanges);

export const optionalRange = fallback(
  optional(rangeValue, defaultTrafficRange),
  defaultTrafficRange,
);

/** The search keys every traffic page shares, for its `validateSearch` schema. */
export const trafficWindowEntries = {
  range: optionalRange,
  from: optionalText,
  to: optionalText,
  gateway: optionalText,
};

/** The shared keys of a page's search, for links that keep the window while changing page. */
export function windowOf(search: TrafficWindowSearch): TrafficWindowSearch {
  return { range: search.range, from: search.from, to: search.to, gateway: search.gateway };
}
