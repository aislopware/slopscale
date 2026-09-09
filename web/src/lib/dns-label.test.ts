import { describe, expect, it } from "vitest";

import { dnsLabelIssue } from "~/lib/dns-label.ts";

describe(dnsLabelIssue, () => {
  it("accepts letters, digits and inner dashes", () => {
    expect(dnsLabelIssue("alice-mbp-2")).toBeNull();
    expect(dnsLabelIssue("A1")).toBeNull();
    expect(dnsLabelIssue("7")).toBeNull();
  });

  it("names the first boundary or character rule the label breaks", () => {
    expect(dnsLabelIssue("")).toBe("empty DNS label");
    expect(dnsLabelIssue("-lead")).toBe("must start with a letter or number");
    expect(dnsLabelIssue("trail-")).toBe("must end with a letter or number");
    expect(dnsLabelIssue("under_score")).toBe('contains invalid character "_"');
    expect(dnsLabelIssue("with space")).toBe('contains invalid character " "');
  });

  it("reports a label that is too long", () => {
    expect(dnsLabelIssue("a".repeat(64))).toBe("DNS label is longer than 63 characters");
  });
});
