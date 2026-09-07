const millisPerSecond = 1000;
const secondsPerMinute = 60;
const minutesPerHour = 60;
const hoursPerDay = 24;
const daysPerWeek = 7;
const daysPerMonth = 30;
const daysPerYear = 365;

const second = millisPerSecond;
const minute = secondsPerMinute * second;
const hour = minutesPerHour * minute;
const day = hoursPerDay * hour;
const week = daysPerWeek * day;
const month = daysPerMonth * day;
const year = daysPerYear * day;

const units: readonly (readonly [Intl.RelativeTimeFormatUnit, number])[] = [
  ["year", year],
  ["month", month],
  ["week", week],
  ["day", day],
  ["hour", hour],
  ["minute", minute],
];

const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
const absolute = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });
const dateOnly = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

/** Parses an RFC 3339 timestamp; the API sends null or the zero time for "never". */
export function parseTime(value: string | null | undefined): Date | null {
  if (value === null || value === undefined || value === "") {
    return null;
  }

  const date = new Date(value);

  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) {
    return null;
  }

  return date;
}

/** "3 minutes ago", "in 2 days", "just now". */
export function formatRelative(date: Date, now: Date = new Date()): string {
  const delta = date.getTime() - now.getTime();

  for (const [unit, size] of units) {
    if (Math.abs(delta) >= size) {
      return relative.format(Math.round(delta / size), unit);
    }
  }

  return Math.abs(delta) < minute
    ? "just now"
    : relative.format(Math.round(delta / minute), "minute");
}

export function formatAbsolute(date: Date): string {
  return absolute.format(date);
}

export function formatDate(date: Date): string {
  return dateOnly.format(date);
}

export function isPast(date: Date | null, now: Date = new Date()): boolean {
  return date !== null && date.getTime() <= now.getTime();
}

/** Subtracts a whole number of hours from now, as the API's RFC 3339 string. */
export function hoursAgo(hours: number): string {
  return new Date(Date.now() - hours * hour).toISOString();
}

/** Adds a whole number of days to now, as the API's RFC 3339 string. */
export function daysFromNow(days: number): string {
  return new Date(Date.now() + days * day).toISOString();
}

function pad(part: number): string {
  return String(part).padStart(2, "0");
}

/** The value a datetime-local input shows for an instant; empty for none. */
export function toLocalInput(value: string | null | undefined): string {
  const date = parseTime(value);

  if (date === null) {
    return "";
  }

  const calendar = [date.getFullYear(), pad(date.getMonth() + 1), pad(date.getDate())].join("-");
  const clock = [pad(date.getHours()), pad(date.getMinutes())].join(":");

  return `${calendar}T${clock}`;
}

/**
 * The instant a datetime-local input holds, as the API's RFC 3339 string; undefined when empty or
 * invalid.
 */
export function fromLocalInput(value: string): string | undefined {
  if (value === "") {
    return undefined;
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

/** Durations in seconds, the unit the API counts access requests in. */
export const minuteSeconds = secondsPerMinute;
export const hourSeconds = minutesPerHour * minuteSeconds;
export const daySeconds = hoursPerDay * hourSeconds;

/** "2 h", "3 d", "45 min": a duration in seconds for a table cell. */
export function formatDuration(seconds: number): string {
  if (seconds >= daySeconds && seconds % daySeconds === 0) {
    return `${seconds / daySeconds} d`;
  }

  if (seconds >= hourSeconds && seconds % hourSeconds === 0) {
    return `${seconds / hourSeconds} h`;
  }

  return `${Math.round(seconds / minuteSeconds)} min`;
}
