import { describe, expect, it } from "vitest";

import { parseExpressions, scheduleSummary } from "~/components/access/posture-model.ts";

describe(scheduleSummary, () => {
  it("is empty without a schedule", () => {
    expect(scheduleSummary()).toBe("");
  });

  it("collapses consecutive days and defaults the zone to UTC", () => {
    expect(
      scheduleSummary({ days: ["mon", "tue", "wed", "fri"], start: "09:00", end: "18:00" }),
    ).toBe("Mon–Wed, Fri 09:00–18:00 UTC");
  });

  it("names every day and keeps the zone", () => {
    expect(
      scheduleSummary({
        days: ["sun", "sat", "mon", "tue", "wed", "thu", "fri"],
        start: "22:00",
        end: "06:00",
        timezone: "Asia/Ho_Chi_Minh",
      }),
    ).toBe("Every day 22:00–06:00 Asia/Ho_Chi_Minh");
  });

  it("lists two days instead of a range", () => {
    expect(scheduleSummary({ days: ["sat", "sun"], start: "00:00", end: "23:59" })).toBe(
      "Sat, Sun 00:00–23:59 UTC",
    );
  });
});

describe(parseExpressions, () => {
  it("drops blank lines and trims", () => {
    expect(parseExpressions("  node:os == 'macos' \n\n\ncustom:x IS SET\n")).toStrictEqual([
      "node:os == 'macos'",
      "custom:x IS SET",
    ]);
  });
});
