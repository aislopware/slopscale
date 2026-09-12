import { describe, expect, it } from "vitest";

import { hueOf, seededColours, seededGradient } from "~/lib/hue.ts";

describe(hueOf, () => {
  // The colour is a property of the seed, not of the render, so a name is the same colour in
  // every list and on every visit.
  it("is the same for the same seed and spread out for different ones", () => {
    expect(hueOf("alice@example.com")).toBe(hueOf("alice@example.com"));

    const hues = ["alice", "bob", "carol", "dave", "erin"].map((name) => hueOf(name));

    expect(new Set(hues).size).toBe(hues.length);
    for (const hue of hues) {
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });

  // The names people tell apart the least must not get the marks that differ the least: a
  // one-character change moves the hue as far as an unrelated name would.
  it("moves a near-identical name across the wheel", () => {
    const pairs = [
      ["Cong Tran", "Cong Tram"],
      ["Nguyen Van A", "Nguyen Van B"],
      ["hieu", "hieu2"],
    ] as const;

    for (const [one, other] of pairs) {
      const apart = Math.abs(hueOf(one) - hueOf(other));

      expect(Math.min(apart, 360 - apart)).toBeGreaterThanOrEqual(10);
    }
  });

  it("snaps to the palette's steps when asked, so two different hues are far apart", () => {
    const hues = ["tag:web", "tag:ci", "tag:prod", "tag:db", "tag:office"].map((tag) =>
      hueOf(tag, 12),
    );

    for (const hue of hues) {
      expect(hue % 30).toBe(0);
    }
  });
});

describe(seededColours, () => {
  it("pairs a light and a dark colour per theme from the seed's hue", () => {
    const { backgroundColor, color } = seededColours("tag:web", 12);

    expect(backgroundColor).toMatch(/^light-dark\(oklch\(.+\), oklch\(.+\)\)$/u);
    expect(color).toContain(`${hueOf("tag:web", 12)}`);
  });
});

describe(seededGradient, () => {
  it("blends two hues of the seed and inks the text from the first", () => {
    const { backgroundImage, color } = seededGradient("Alice Nguyen");

    expect(backgroundImage).toMatch(
      /^linear-gradient\(\d+deg, light-dark\(.+\), light-dark\(.+\)\)$/u,
    );
    expect(color).toContain(`${hueOf("Alice Nguyen")}`);
    expect(seededGradient("Bob Tran").backgroundImage).not.toBe(backgroundImage);
  });
});
