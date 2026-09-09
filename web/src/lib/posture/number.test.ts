import { describe, expect, it } from "vitest";

import { isGoFloat, parseGoFloat } from "~/lib/posture/number.ts";

// Each case was run through strconv.ParseFloat(s, 64) in Go 1.25.
const accepted = [
  "1",
  "-1",
  "+1",
  "1.5",
  ".5",
  "5.",
  "1e5",
  "1E-5",
  "1.5e+3",
  "1_000",
  "0x1p2",
  "0x1.8p1",
  "0x.8p1",
  "0X1P-2",
  "inf",
  "+Inf",
  "-infinity",
  "NaN",
  "nan",
  "1e1_0",
  "00",
  "007",
  "-.5",
];

const rejected = [
  "",
  "1__0",
  "_1",
  "1_",
  "0x10",
  "0x1",
  "0b10",
  "0o7",
  "1e",
  "e5",
  "1.2.3",
  "1e5.5",
  "0x1p",
  "1_e5",
  "1e_5",
  "1f",
  "Infinity1",
];

describe(isGoFloat, () => {
  it.each(accepted)("accepts %s as Go does", (word) => {
    expect(isGoFloat(word)).toBe(true);
  });

  it.each(rejected)("rejects %s as Go does", (word) => {
    expect(isGoFloat(word)).toBe(false);
  });
});

describe(parseGoFloat, () => {
  it("reads decimal, hex and the named values the way Go does", () => {
    expect(parseGoFloat("1_000.5")).toBe(1000.5);
    expect(parseGoFloat("0x1.8p1")).toBe(3);
    expect(parseGoFloat("-0X1P-2")).toBe(-0.25);
    expect(parseGoFloat("-infinity")).toBe(Number.NEGATIVE_INFINITY);
    expect(parseGoFloat("nan")).toBeNaN();
  });

  it("gives null for what Go refuses", () => {
    expect(parseGoFloat("0b10")).toBeNull();
    expect(parseGoFloat("0x10")).toBeNull();
  });
});
