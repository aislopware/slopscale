import { describe, expect, it } from "vitest";

import { retentionError } from "~/components/traffic/settings-section.tsx";

const stored = { minuteHours: "48", hourDays: "30", dayDays: "400" };

describe(retentionError, () => {
  it("lets the defaults through", () => {
    expect(retentionError(stored)).toBeNull();
  });

  it("holds each field to what the server accepts", () => {
    expect(retentionError({ ...stored, minuteHours: "" })).toBe(
      "Keep per-minute totals for 1 to 168 hours.",
    );
    expect(retentionError({ ...stored, hourDays: "1.5" })).toBe(
      "Keep hourly data for 1 to 90 days.",
    );
    expect(retentionError({ ...stored, dayDays: "4000" })).toBe(
      "Keep daily data for 1 to 3650 days.",
    );
  });

  it("refuses a finer resolution that outlives a coarser one", () => {
    expect(retentionError({ ...stored, minuteHours: "72", hourDays: "2" })).toBe(
      "Per-minute totals cannot outlive the hourly data.",
    );
    expect(retentionError({ ...stored, hourDays: "60", dayDays: "30" })).toBe(
      "Hourly data cannot outlive the daily data.",
    );
  });
});
