import { describe, expect, it } from "vitest";

import {
  countryName,
  formatBytes,
  formatRate,
  formatScaled,
  networkLabel,
  networkName,
  portLabel,
  rateScale,
  serviceName,
  shareLabel,
} from "~/components/traffic/format.ts";

describe(formatBytes, () => {
  it("counts in binary units with one decimal below ten", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1536)).toBe("1.5 KiB");
    expect(formatBytes(20 * 1024 ** 3)).toBe("20 GiB");
    expect(formatRate(3 * 1024 ** 2)).toBe("3.0 MiB/s");
  });

  it("moves to the next unit rather than print 1024 of one", () => {
    expect(formatBytes(1024 * 1024 - 1)).toBe("1.0 MiB");
  });

  it("reads a nonsense count as nothing", () => {
    expect(formatBytes(-5)).toBe("0 B");
    expect(formatBytes(Number.NaN)).toBe("0 B");
  });
});

describe(portLabel, () => {
  it("names the protocol and the port, and the service it usually carries", () => {
    expect(portLabel(6, 443)).toBe("TCP 443");
    expect(serviceName(17, 443)).toBe("QUIC");
    expect(serviceName(6, 12_345)).toBe("");
    expect(portLabel(1, 0)).toBe("ICMP");
    expect(portLabel(99, 0)).toBe("IP 99");
  });
});

describe(countryName, () => {
  it("spells a code out and leaves one it does not know alone", () => {
    expect(countryName("vn")).not.toBe("vn");
    expect(countryName("")).toBe("");
    expect(countryName("not a code")).toBe("not a code");
  });
});

describe(networkLabel, () => {
  it("puts the number before the name, and says nothing for no network", () => {
    expect(networkLabel(15_169, "Google LLC")).toBe("AS15169 Google LLC");
    expect(networkLabel(64_512, "")).toBe("AS64512");
    expect(networkLabel(0, "")).toBe("");
  });
});

describe(shareLabel, () => {
  it("rounds to whole percents and keeps a sliver visible", () => {
    expect(shareLabel(1, 4)).toBe("25%");
    expect(shareLabel(1, 1000)).toBe("<1%");
    expect(shareLabel(0, 1000)).toBe("0%");
    expect(shareLabel(5, 0)).toBe("0%");
  });
});

describe(networkName, () => {
  it("leads with the organisation and keeps the registry handle apart", () => {
    expect(networkName("VIETEL-AS-AP Viettel Group")).toStrictEqual({
      org: "Viettel Group",
      handle: "VIETEL-AS-AP",
    });
    expect(networkName("CLOUDFLARENET")).toStrictEqual({ org: "CLOUDFLARENET", handle: "" });
    expect(networkName("Google LLC")).toStrictEqual({ org: "Google LLC", handle: "" });
    expect(networkName("")).toStrictEqual({ org: "", handle: "" });
  });
});

describe(rateScale, () => {
  it("puts every tick of a chart in the unit of its largest rate", () => {
    const scale = rateScale(6.9 * 1024 ** 2);

    expect(scale).toStrictEqual({ unit: "MiB/s", divisor: 1024 ** 2 });
    expect(formatScaled(0.5, scale)).toBe("0.5 MiB/s");
    expect(formatScaled(4, scale)).toBe("4 MiB/s");
    expect(rateScale(900)).toStrictEqual({ unit: "B/s", divisor: 1 });
    expect(rateScale(0)).toStrictEqual({ unit: "B/s", divisor: 1 });
  });
});
