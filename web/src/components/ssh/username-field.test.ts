import { describe, expect, it } from "vitest";

import { usernamePrefillStep } from "~/components/ssh/username-field.tsx";

/** Replays a sequence of field states, carrying the consumed flag the page keeps in a ref. */
function replay(
  steps: readonly { readonly draft: string; readonly suggestions: readonly string[] }[],
): (string | null)[] {
  let used = false;

  return steps.map((step) => {
    const { insert, consumed } = usernamePrefillStep(step.draft, step.suggestions, used);

    used = consumed;

    return insert;
  });
}

describe(usernamePrefillStep, () => {
  it("fills an empty field from the machine's only suggestion", () => {
    expect(usernamePrefillStep("", ["alice"], false)).toStrictEqual({
      insert: "alice",
      consumed: true,
    });
  });

  it("does not pick between accounts, and has nothing to offer without one", () => {
    expect(usernamePrefillStep("", [], false)).toStrictEqual({ insert: null, consumed: false });
    expect(usernamePrefillStep("", ["alice", "root"], false)).toStrictEqual({
      insert: null,
      consumed: false,
    });
  });

  // The server answers first often enough: the session it hands back names an account, and the
  // machine's own suggestion arrives after it. Clearing that answer must not bring another one in.
  it("leaves the field empty after the operator clears the server's prefill", () => {
    const inserts = replay([
      { draft: "", suggestions: [] },
      { draft: "root", suggestions: [] },
      { draft: "root", suggestions: ["alice"] },
      { draft: "", suggestions: ["alice"] },
    ]);

    expect(inserts).toStrictEqual([null, null, null, null]);
  });

  it("inserts the machine's suggestion once, and not again once it is cleared", () => {
    const inserts = replay([
      { draft: "", suggestions: ["alice"] },
      { draft: "alice", suggestions: ["alice"] },
      { draft: "", suggestions: ["alice"] },
    ]);

    expect(inserts).toStrictEqual(["alice", null, null]);
  });

  it("never writes over a field the operator typed in", () => {
    const inserts = replay([
      { draft: "", suggestions: [] },
      { draft: "r", suggestions: [] },
      { draft: "ro", suggestions: ["alice"] },
      { draft: "root", suggestions: ["alice"] },
    ]);

    expect(inserts).toStrictEqual([null, null, null, null]);
  });
});
