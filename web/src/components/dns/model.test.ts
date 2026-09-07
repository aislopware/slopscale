import { describe, expect, it } from "vitest";

import type { DnsSettings } from "~/api/schema.gen.ts";
import {
  cloneSettings,
  isDomain,
  isNameserver,
  parseList,
  recordError,
  splitEntries,
  withRecord,
  withSplit,
  withoutNameserver,
  withoutRecord,
  withoutSplit,
} from "~/components/dns/model.ts";

const base: DnsSettings = {
  nameservers: ["1.1.1.1"],
  overrideLocalDns: false,
  splitNameservers: { "corp.example": ["10.0.0.1"], gone: null },
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
    "https://dns.example/dns-query",
    "tls://dns.example",
  ])("accepts %s", (value) => {
    expect(isNameserver(value)).toBe(true);
  });

  it.each([
    "",
    "one.one.one.one",
    "999.1.1.1",
    "http://dns.example",
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
    expect(recordError({ name: "a.corp", type: "TXT", value: "v=spf1 -all" })).toBeNull();
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
    const next = withRecord(base, { name: "B.Corp.", type: "TXT", value: " hi " }, 0);

    expect(next.extraRecords).toStrictEqual([{ name: "b.corp", type: "TXT", value: "hi" }]);
    expect(
      withRecord(base, { name: "c.corp", type: "", value: "10.0.0.7" }).extraRecords,
    ).toHaveLength(2);
    expect(withoutRecord(base, 0).extraRecords).toStrictEqual([]);
  });

  it("removes a nameserver", () => {
    expect(withoutNameserver(base, "1.1.1.1").nameservers).toStrictEqual([]);
  });

  it("parses lines and commas", () => {
    expect(parseList("1.1.1.1, 1.0.0.1\n\n 9.9.9.9 ")).toStrictEqual([
      "1.1.1.1",
      "1.0.0.1",
      "9.9.9.9",
    ]);
  });
});
