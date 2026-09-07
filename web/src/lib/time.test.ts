import { describe, expect, it } from "vitest";

import { daysFromNow, formatRelative, isPast, parseTime } from "~/lib/time.ts";

describe(parseTime, () => {
  it("treats null, empty and the zero time as never", () => {
    expect(parseTime(null)).toBeNull();
    expect(parseTime("")).toBeNull();
    expect(parseTime("0001-01-01T00:00:00Z")).toBeNull();
  });

  it("parses RFC 3339", () => {
    expect(parseTime("2026-09-07T10:00:00Z")?.toISOString()).toBe("2026-09-07T10:00:00.000Z");
  });
});

describe(formatRelative, () => {
  const now = new Date("2026-09-07T12:00:00Z");

  it("picks the largest whole unit", () => {
    expect(formatRelative(new Date("2026-09-07T11:57:00Z"), now)).toBe("3 minutes ago");
    expect(formatRelative(new Date("2026-09-07T09:00:00Z"), now)).toBe("3 hours ago");
    expect(formatRelative(new Date("2026-09-01T12:00:00Z"), now)).toBe("6 days ago");
    expect(formatRelative(new Date("2026-11-07T12:00:00Z"), now)).toBe("in 2 months");
  });

  it("says just now inside a minute", () => {
    expect(formatRelative(new Date("2026-09-07T11:59:30Z"), now)).toBe("just now");
  });
});

describe(isPast, () => {
  const now = new Date("2026-09-07T12:00:00Z");

  it("is false for never", () => {
    expect(isPast(null, now)).toBe(false);
  });

  it("compares against the given clock", () => {
    expect(isPast(new Date("2026-09-07T11:59:59Z"), now)).toBe(true);
    expect(isPast(new Date("2026-09-07T12:00:01Z"), now)).toBe(false);
  });
});

describe(daysFromNow, () => {
  it("returns an RFC 3339 timestamp in the future", () => {
    const later = new Date(daysFromNow(7));

    expect(later.getTime()).toBeGreaterThan(Date.now());
  });
});
