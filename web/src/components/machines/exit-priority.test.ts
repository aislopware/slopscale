import { describe, expect, it } from "vitest";

import { globalExitNodeMessage } from "~/components/machines/mutations.ts";
import { parsePriority } from "~/components/machines/routes.tsx";

describe(parsePriority, () => {
  it("accepts whole numbers, 0 included", () => {
    expect(parsePriority("20")).toBe(20);
    expect(parsePriority(" 0 ")).toBe(0);
  });

  it("rejects blanks, negatives, fractions and words", () => {
    expect(parsePriority("")).toBeNull();
    expect(parsePriority("-1")).toBeNull();
    expect(parsePriority("1.5")).toBeNull();
    expect(parsePriority("ten")).toBeNull();
  });
});

describe(globalExitNodeMessage, () => {
  it("says what the mark became, with the priority when it has one", () => {
    expect(globalExitNodeMessage({ globalExitNode: true, exitNodePriority: 0 })).toBe(
      "Marked as global exit node",
    );
    expect(globalExitNodeMessage({ globalExitNode: true, exitNodePriority: 20 })).toBe(
      "Global exit node, priority 20",
    );
    expect(globalExitNodeMessage({ globalExitNode: false, exitNodePriority: 0 })).toBe(
      "No longer a global exit node",
    );
  });
});
