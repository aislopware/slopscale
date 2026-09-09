import { describe, expect, it } from "vitest";

import { shouldPrefillUsername } from "~/components/ssh/username-field.tsx";

describe(shouldPrefillUsername, () => {
  it("fills an empty field from the machine's only suggestion", () => {
    expect(shouldPrefillUsername("", ["alice"], false)).toBe(true);
  });

  // The server's guess and the operator's own typing land in the same field, and whichever got
  // there first is the one that meant something.
  it("never writes over a field that already says something", () => {
    expect(shouldPrefillUsername("root", ["alice"], false)).toBe(false);
  });

  it("fills once only, so a field cleared to type another account stays cleared", () => {
    expect(shouldPrefillUsername("", ["alice"], true)).toBe(false);
  });

  it("does not pick between accounts, and has nothing to offer without one", () => {
    expect(shouldPrefillUsername("", [], false)).toBe(false);
    expect(shouldPrefillUsername("", ["alice", "root"], false)).toBe(false);
  });
});
