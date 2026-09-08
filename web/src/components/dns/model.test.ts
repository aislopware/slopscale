import { describe, expect, it } from "vitest";

import type { DnsSettings } from "~/api/schema.gen.ts";
import {
  cloneSettings,
  isDomain,
  isNameserver,
  parseList,
  recordError,
  splitEntries,
  splitKeptWithExitNode,
  withOverrideLocalDns,
  withRecord,
  withSplit,
  withSplitUseWithExitNode,
  withUseWithExitNode,
  withoutNameserver,
  withoutRecord,
  withoutSplit,
} from "~/components/dns/model.ts";

const base: DnsSettings = {
  nameservers: ["1.1.1.1"],
  overrideLocalDns: false,
  splitNameservers: { "corp.example": ["10.0.0.1"], gone: null },
  useWithExitNode: [],
  splitUseWithExitNode: {},
  searchDomains: ["lab.example"],
  extraRecords: [{ name: "a.corp", type: "A", value: "10.0.0.5" }],
};

describe(isNameserver, () => {
  it.each([
    "1.1.1.1",
    "2606:4700:4700::1111",
    "::1",
    "::",
    "1:2:3:4:5:6:7:8",
    "10.0.0.1:5353",
    "[fd00::1]:53",
    "https://dns.nextdns.io/abc123",
  ])("accepts %s", (value) => {
    expect(isNameserver(value)).toBe(true);
  });

  it.each([
    "",
    "one.one.one.one",
    "999.1.1.1",
    "http://dns.example",
    "tls://dns.example",
    "1.1.1.1:70000",
    "1:2:3:4:5:6:7:8:9",
    "1::2::3",
    "fd00",
  ])("rejects %s", (value) => {
    expect(isNameserver(value)).toBe(false);
  });
});

describe(isDomain, () => {
  it("accepts labels with digits, dashes and underscores", () => {
    expect(isDomain("corp.example.com")).toBe(true);
    expect(isDomain("_acme-challenge.corp")).toBe(true);
  });

  it("rejects spaces, leading dashes and empty labels", () => {
    expect(isDomain("not a domain")).toBe(false);
    expect(isDomain("-bad.example")).toBe(false);
    expect(isDomain("a..b")).toBe(false);
    expect(isDomain("")).toBe(false);
  });
});

describe(recordError, () => {
  it("accepts a value that matches the type", () => {
    expect(recordError({ name: "a.corp", type: "", value: "10.0.0.5" })).toBeNull();
    expect(recordError({ name: "a.corp", type: "AAAA", value: "fd00::5" })).toBeNull();
  });

  it("names what is wrong", () => {
    expect(recordError({ name: "a.corp", type: "", value: "x" })).toMatch(/IP address/v);
    expect(recordError({ name: "a.corp", type: "A", value: "fd00::5" })).toMatch(/IPv4/v);
    expect(recordError({ name: "a.corp", type: "AAAA", value: "10.0.0.5" })).toMatch(/IPv6/v);
    expect(recordError({ name: "", type: "A", value: "10.0.0.5" })).toMatch(/name/v);
  });
});

describe("editing helpers", () => {
  it("copies deeply and drops null split entries", () => {
    const copy = cloneSettings(base);

    expect(copy).not.toBe(base);
    expect(copy.splitNameservers).toStrictEqual({ "corp.example": ["10.0.0.1"] });
    expect(splitEntries(base)).toStrictEqual([["corp.example", ["10.0.0.1"]]]);
  });

  it("renames a split domain by dropping the old key", () => {
    const next = withSplit(base, {
      domain: "new.example",
      servers: ["10.0.0.2"],
      previous: "corp.example",
    });

    expect(next.splitNameservers).toStrictEqual({ "new.example": ["10.0.0.2"] });
    expect(withoutSplit(next, "new.example").splitNameservers).toStrictEqual({});
    expect(base.splitNameservers["corp.example"]).toStrictEqual(["10.0.0.1"]);
  });

  it("replaces a record in place and removes by index", () => {
    const next = withRecord(base, { name: "B.Corp.", type: "A", value: " 10.0.0.9 " }, 0);

    expect(next.extraRecords).toStrictEqual([{ name: "b.corp", type: "A", value: "10.0.0.9" }]);
    expect(
      withRecord(base, { name: "c.corp", type: "", value: "10.0.0.7" }).extraRecords,
    ).toHaveLength(2);
    expect(withoutRecord(base, 0).extraRecords).toStrictEqual([]);
  });

  it("removes a nameserver and its exit node mark", () => {
    const kept = withUseWithExitNode({ ...base, overrideLocalDns: true }, "1.1.1.1", true);

    expect(kept.useWithExitNode).toStrictEqual(["1.1.1.1"]);
    expect(withUseWithExitNode(kept, "1.1.1.1", true).useWithExitNode).toStrictEqual(["1.1.1.1"]);
    expect(withoutNameserver(kept, "1.1.1.1")).toMatchObject({
      nameservers: [],
      useWithExitNode: [],
    });
  });

  it("drops the exit node marks when the override goes off", () => {
    const kept = withUseWithExitNode({ ...base, overrideLocalDns: true }, "1.1.1.1", true);

    expect(withOverrideLocalDns(kept, false).useWithExitNode).toStrictEqual([]);
    expect(withOverrideLocalDns(kept, true).useWithExitNode).toStrictEqual(["1.1.1.1"]);
  });

  it("keeps a split domain with all of its resolvers or none", () => {
    const kept = withSplitUseWithExitNode(base, "corp.example", true);

    expect(kept.splitUseWithExitNode).toStrictEqual({ "corp.example": ["10.0.0.1"] });
    expect(splitKeptWithExitNode(kept, "corp.example")).toBe(true);
    expect(splitKeptWithExitNode(base, "corp.example")).toBe(false);
    expect(
      withSplitUseWithExitNode(kept, "corp.example", false).splitUseWithExitNode,
    ).toStrictEqual({});
  });

  it("carries the exit node mark through split edits", () => {
    const kept = withSplitUseWithExitNode(base, "corp.example", true);
    const edited = withSplit(kept, { domain: "corp.example", servers: ["10.0.0.1", "10.0.0.2"] });

    expect(edited.splitUseWithExitNode).toStrictEqual({ "corp.example": ["10.0.0.1", "10.0.0.2"] });
    expect(
      withSplit(kept, { domain: "new.example", servers: ["10.0.0.3"], previous: "corp.example" })
        .splitUseWithExitNode,
    ).toStrictEqual({ "new.example": ["10.0.0.3"] });
    expect(withoutSplit(kept, "corp.example").splitUseWithExitNode).toStrictEqual({});
  });

  it("parses lines and commas", () => {
    expect(parseList("1.1.1.1, 1.0.0.1\n\n 9.9.9.9 ")).toStrictEqual([
      "1.1.1.1",
      "1.0.0.1",
      "9.9.9.9",
    ]);
  });
});
