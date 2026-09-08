import { describe, expect, it } from "vitest";

import { aliasKind, isDefaultRoute, isPrefix, splitPorts } from "~/components/policy/aliases.ts";

describe(isPrefix, () => {
  it.each([
    "10.0.0.1",
    "10.0.0.0/8",
    "0.0.0.0/0",
    "fd7a:115c:a1e0::1",
    "2001:0db8:0000:0000:0000:0000:0000:0001",
    "::ffff:192.0.2.1",
    "fd7a:115c:a1e0::/48",
    "::/0",
    "fe80::1%eth0",
  ])("accepts %s as netip does", (text) => {
    expect(isPrefix(text)).toBe(true);
  });

  it.each([
    "10.0.0.0/33",
    "10.0.0.0/08",
    "fd7a::/129",
    "fe80::1%eth0/64",
    "fe80::1%",
    "10.0.0.256",
    "example.com",
    "",
  ])("rejects %s", (text) => {
    expect(isPrefix(text)).toBe(false);
  });
});

describe(aliasKind, () => {
  it("tells an address from a name", () => {
    expect(aliasKind("::ffff:192.0.2.1")).toBe("prefix");
    expect(aliasKind("group:eng")).toBe("group");
    expect(aliasKind("tag:web")).toBe("tag");
  });
});

describe(isDefaultRoute, () => {
  it("matches only the two whole-internet ranges", () => {
    expect(isDefaultRoute("0.0.0.0/0")).toBe(true);
    expect(isDefaultRoute("::/0")).toBe(true);
    expect(isDefaultRoute("10.0.0.0/0")).toBe(true);
    expect(isDefaultRoute("10.0.0.0/8")).toBe(false);
  });
});

describe(splitPorts, () => {
  it("takes the ports after the last colon of an IPv6 address", () => {
    expect(splitPorts("2001:db8::1:443")).toStrictEqual({ alias: "2001:db8::1", ports: "443" });
    expect(splitPorts("tag:web:22")).toStrictEqual({ alias: "tag:web", ports: "22" });
  });
});
