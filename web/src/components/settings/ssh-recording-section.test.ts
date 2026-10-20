import { describe, expect, it } from "vitest";

import { parseRecorders, recordersIssue } from "~/components/settings/ssh-recording-section.tsx";

describe(recordersIssue, () => {
  it("accepts tags, host names and tailnet addresses", () => {
    expect(
      recordersIssue(parseRecorders("tag:recorder, recorder-1 100.64.0.9 fd7a::/48")),
    ).toBeNull();
    expect(recordersIssue([])).toBeNull();
  });

  it("refuses users, groups and autogroups by name", () => {
    expect(recordersIssue(["tag:recorder", "alice@"])).toBe(
      "alice@ is not a recorder. Use a tag, a host name or a tailnet address.",
    );
    expect(recordersIssue(["group:ops"])).toMatch(/^group:ops is not a recorder/v);
    expect(recordersIssue(["autogroup:member"])).toMatch(/^autogroup:member is not a recorder/v);
  });

  it("refuses a tag without a name", () => {
    expect(recordersIssue(parseRecorders("tag:"))).toBe("A tag needs a name after tag:.");
  });
});
