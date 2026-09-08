import { describe, expect, it } from "vitest";

import { isIp, isIpv4, isIpv6 } from "~/lib/ip.ts";

describe(isIpv4, () => {
  it.each(["0.0.0.0", "10.0.0.1", "255.255.255.255"])("accepts %s", (text) => {
    expect(isIpv4(text)).toBe(true);
  });

  it.each(["10.0.0", "10.0.0.1.2", "256.0.0.1", "01.2.3.4", "1.2.3.", "a.b.c.d", ""])(
    "rejects %s",
    (text) => {
      expect(isIpv4(text)).toBe(false);
    },
  );
});

describe(isIpv6, () => {
  it.each([
    "::",
    "::1",
    "fd7a:115c:a1e0::1",
    "2001:0db8:0000:0000:0000:0000:0000:0001",
    "2001:DB8::1",
    "1:2:3:4:5:6:7:8",
    "1:2:3:4:5:6:7::",
    "::ffff:192.0.2.1",
    "::192.0.2.1",
    "64:ff9b::192.0.2.33",
    "1:2:3:4:5:6:192.0.2.1",
  ])("accepts %s", (text) => {
    expect(isIpv6(text)).toBe(true);
  });

  it.each([
    "1:2:3:4:5:6:7",
    "1:2:3:4:5:6:7:8:9",
    "1:2:3:4:5:6:7:8::",
    "1::2::3",
    ":1:2:3:4:5:6:7:8",
    "1:2:3:4:5:6:7:8:",
    "12345::1",
    "g::1",
    "192.0.2.1::",
    "1:2:3:4:5:6:7:192.0.2.1",
    "::ffff:192.0.2.1:1",
    "::ffff:192.0.2.01",
    "fe80::1%eth0",
    "10.0.0.1",
    "",
  ])("rejects %s", (text) => {
    expect(isIpv6(text)).toBe(false);
  });
});

describe(isIp, () => {
  it("takes either family", () => {
    expect(isIp("10.0.0.1")).toBe(true);
    expect(isIp("::1")).toBe(true);
    expect(isIp("example.com")).toBe(false);
  });
});
