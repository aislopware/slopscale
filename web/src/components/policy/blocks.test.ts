import { describe, expect, it } from "vitest";

import { policyBlocks, toJson } from "~/components/policy/blocks.ts";

const policy = `{
  // group:ignored lives in a comment and must not count.
  "groups": { "group:admins": ["dev@", "jane.doe@"] },
  "tagOwners": { "tag:gateway": ["group:admins"] },
  "acls": [
    { "action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"] },
    { "action": "accept", "src": ["group:admins"], "dst": ["tag:gateway:22,443"] },
  ],
}
`;

describe(policyBlocks, () => {
  it("reads the names out of a valid policy", () => {
    const blocks = policyBlocks(policy);

    expect(blocks.valid).toBe(true);
    expect(blocks.groups).toStrictEqual(["group:admins"]);
    expect(blocks.tags).toStrictEqual(["tag:gateway"]);
    expect(blocks.autogroups).toStrictEqual(["autogroup:member", "autogroup:self"]);
  });

  it("scans the text when the draft does not parse", () => {
    const blocks = policyBlocks('{ "groups": { "group:admins": [ ');

    expect(blocks.valid).toBe(false);
    expect(blocks.groups).toStrictEqual(["group:admins"]);
  });

  it("is empty for an empty draft", () => {
    const blocks = policyBlocks("  \n ");

    expect(blocks.valid).toBe(true);
    expect(blocks.groups).toStrictEqual([]);
    expect(blocks.tags).toStrictEqual([]);
    expect(blocks.autogroups).toStrictEqual([]);
  });

  it("keeps comment and comma characters that live inside strings", () => {
    const json = toJson('{ "note": "// not a comment, [x]", }');

    expect(JSON.parse(json)).toStrictEqual({ note: "// not a comment, [x]" });
  });
});
