import { describe, expect, it } from "vitest";

import { parseHujson } from "~/lib/hujson/ast.ts";

function messages(text: string): string[] {
  return parseHujson(text).issues.map((issue) => issue.message);
}

describe(parseHujson, () => {
  // A stray string already carries its quotes; wrapping it in more read as `Unexpected ""nope""`.
  it("names a stray string with its own quotes, once", () => {
    const found = messages('["a" "nope"]');

    expect(found.some((message) => message.includes('"nope"'))).toBe(true);
    expect(found.join("\n")).not.toContain('""');
  });

  it("quotes stray text that is not a string", () => {
    expect(messages('{"a": 1 nope}').join("\n")).toContain('Unexpected "nope"');
  });

  it("parses a valid file without issues", () => {
    expect(messages('{"a": [1, "b"], // note\n}')).toStrictEqual([]);
  });
});
