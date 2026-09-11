import { describe, expect, it } from "vitest";

import { knownTags, normalizeTag, parseTagList, splitTag, tagError } from "~/lib/tag.ts";

describe(splitTag, () => {
  it("parts the prefix from the name and leaves a bare name alone", () => {
    expect(splitTag("tag:server")).toStrictEqual({ prefix: "tag:", name: "server" });
    expect(splitTag("server")).toStrictEqual({ prefix: "", name: "server" });
  });
});

describe(normalizeTag, () => {
  it("adds the prefix, lower-cases and trims, and leaves nothing as nothing", () => {
    expect(normalizeTag("  Server ")).toBe("tag:server");
    expect(normalizeTag("tag:prod")).toBe("tag:prod");
    expect(normalizeTag("TAG:Prod")).toBe("tag:prod");
    expect(normalizeTag("   ")).toBe("");
  });
});

describe(tagError, () => {
  it("takes what the server takes", () => {
    expect(tagError("tag:server")).toBeNull();
    expect(tagError("tag:web-1.eu_west")).toBeNull();
  });

  it("names what is wrong, the way the server would refuse it", () => {
    expect(tagError("tag:")).toContain("Enter a tag name");
    expect(tagError("tag:has space")).toContain("spaces");
    expect(tagError("tag:1abc")).toContain("starts with a letter");
    expect(tagError("tag:a/b")).toContain("lower-case letters");
  });
});

describe(parseTagList, () => {
  it("splits a pasted list on commas, spaces and newlines and keeps each tag once", () => {
    expect(parseTagList("tag:ci, prod\nCI  tag:ci")).toStrictEqual(["tag:ci", "tag:prod"]);
    expect(parseTagList(" , ")).toStrictEqual([]);
  });
});

describe(knownTags, () => {
  it("unions the sources, drops what is not a tag and sorts", () => {
    expect(knownTags([["tag:web", "tag:ci"], ["tag:ci", "group:ops"], []])).toStrictEqual([
      "tag:ci",
      "tag:web",
    ]);
  });
});
