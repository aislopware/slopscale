import type { AccessRule, Posture } from "~/api/queries.ts";

export const weekdays = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] as const;
export type Weekday = (typeof weekdays)[number];

export const weekdayLabels: Record<Weekday, string> = {
  mon: "Mon",
  tue: "Tue",
  wed: "Wed",
  thu: "Thu",
  fri: "Fri",
  sat: "Sat",
  sun: "Sun",
};

export function isWeekday(value: string): value is Weekday {
  return weekdays.some((day) => day === value);
}

/** The rules that require the posture. */
export function rulesUsingPosture(rules: readonly AccessRule[], posture: Posture): AccessRule[] {
  return rules.filter((rule) => rule.postureIds.includes(posture.id));
}

export function postureName(postures: readonly Posture[], id: string): string {
  return postures.find((posture) => posture.id === id)?.name ?? `Posture ${id}`;
}

/** One expression per line, blank lines dropped. */
export function parseExpressions(text: string): string[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line !== "");
}

const clock = /^(?:[01]\d|2[0-3]):[0-5]\d$/v;

export function isClock(value: string): boolean {
  return clock.test(value);
}

/** A schedule as one line: "Mon–Fri 09:00–17:30 Asia/Ho_Chi_Minh". */
export function scheduleSummary(schedule?: Posture["schedule"]): string {
  if (schedule === undefined) {
    return "";
  }

  const days = weekdays.filter((day) => schedule.days.includes(day));
  const dayText = days.length === weekdays.length ? "Every day" : dayRanges(days);
  const zone =
    schedule.timezone === undefined || schedule.timezone === "" ? "UTC" : schedule.timezone;

  return `${dayText} ${schedule.start}–${schedule.end} ${zone}`;
}

const rangeMinimum = 3;

/** Consecutive days collapse to a range: mon,tue,wed,fri becomes "Mon–Wed, Fri". */
function dayRanges(days: readonly Weekday[]): string {
  const runs: Weekday[][] = [];

  for (const day of days) {
    const run = runs.at(-1);
    const previous = run?.at(-1);

    if (run !== undefined && previous !== undefined && follows(previous, day)) {
      run.push(day);
    } else {
      runs.push([day]);
    }
  }

  return runs.map((run) => rangeText(run)).join(", ");
}

function follows(previous: Weekday, day: Weekday): boolean {
  return weekdays.indexOf(day) === weekdays.indexOf(previous) + 1;
}

function rangeText(run: readonly Weekday[]): string {
  const labels = run.map((day) => weekdayLabels[day]);

  return run.length >= rangeMinimum
    ? `${labels[0] ?? ""}–${labels.at(-1) ?? ""}`
    : labels.join(", ");
}

/** Example expressions the editor offers, one per attribute the server derives. */
export const expressionExamples: readonly { label: string; expression: string }[] = [
  { label: "Current client", expression: "node:tsVersion >= '1.80'" },
  { label: "Stable track", expression: "node:tsReleaseTrack == 'stable'" },
  { label: "macOS or Windows", expression: "node:os IN ['macos', 'windows']" },
  { label: "Auto-update on", expression: "node:tsAutoUpdate == true" },
  { label: "Known serial", expression: "node:serialNumber IN ['C02XYZ123']" },
  { label: "Custom marker", expression: "custom:oncall == true" },
  { label: "Office network", expression: "ip:address IN ['203.0.113.0/24']" },
  { label: "Country", expression: "ip:country IN ['VN', 'SG']" },
];
