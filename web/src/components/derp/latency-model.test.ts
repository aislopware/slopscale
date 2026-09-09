import { describe, expect, it } from "vitest";

import type { DerpLatencyRegion, DerpLatencyReport, NodeNetInfo } from "~/api/schema.gen.ts";
import {
  barPercent,
  homeLatency,
  linkLabel,
  machineHome,
  machineRows,
  maxPreferredBy,
  msLabel,
  msValue,
  natLabel,
  natTone,
  noValue,
  otherLatencies,
  portMapLabel,
  portMapProtocols,
  rangeValue,
  yesNo,
} from "~/components/derp/latency-model.ts";

function region(overrides: Partial<DerpLatencyRegion> = {}): DerpLatencyRegion {
  return {
    regionId: 1,
    code: "nyc",
    name: "New York City",
    inMap: true,
    preferredBy: 0,
    samples: 0,
    minMs: 0,
    medianMs: 0,
    p90Ms: 0,
    maxMs: 0,
    ...overrides,
  };
}

function netInfo(overrides: Partial<NodeNetInfo> = {}): NodeNetInfo {
  return {
    preferredDerp: 0,
    preferredDerpName: "",
    latency: [],
    linkType: "",
    mappingVariesByDestIp: null,
    workingIpv6: null,
    workingUdp: null,
    havePortMap: false,
    upnp: null,
    pmp: null,
    pcp: null,
    ...overrides,
  };
}

describe("round trips", () => {
  it("shows one decimal", () => {
    expect(msValue(12.345)).toBe("12.3");
    expect(msLabel(12.345)).toBe("12.3 ms");
  });

  it("shows a dash when nothing measured it", () => {
    expect(msValue(0)).toBe(noValue);
    expect(msLabel(0)).toBe(noValue);
  });

  it("puts the best and the worst in one cell", () => {
    expect(rangeValue(region({ samples: 3, minMs: 8.11, maxMs: 40.25 }))).toBe("8.1 / 40.3");
  });

  it("has no range without samples", () => {
    expect(rangeValue(region({ minMs: 8, maxMs: 40 }))).toBe(noValue);
  });
});

describe("the preferred bar", () => {
  it("scales to the busiest region", () => {
    const regions = [region({ preferredBy: 3 }), region({ regionId: 2, preferredBy: 12 })];

    expect(maxPreferredBy(regions)).toBe(12);
    expect(barPercent(3, 12)).toBe(25);
  });

  it("draws nothing when nothing homes anywhere", () => {
    expect(maxPreferredBy([])).toBe(0);
    expect(barPercent(0, 0)).toBe(0);
  });
});

describe("a machine's home region", () => {
  const report: DerpLatencyReport = {
    reporting: 1,
    silent: 0,
    hardNat: 0,
    regions: [region({ regionId: 7, code: "sgp", name: "Singapore" })],
    machines: [],
  };
  const machine = {
    nodeId: "1",
    name: "laptop",
    online: true,
    preferredDerp: 7,
    homeMs: 4,
    hardNat: false,
    linkType: "wifi",
  };

  it("is looked up in the report's regions", () => {
    expect(machineHome(report, machine)?.code).toBe("sgp");
  });

  it("is absent while the client has not picked one", () => {
    expect(machineHome(report, { ...machine, preferredDerp: 0 })).toBeUndefined();
  });

  it("is resolved once per row for the table", () => {
    const rows = machineRows({
      ...report,
      machines: [machine, { ...machine, nodeId: "2", preferredDerp: 0 }],
    });

    expect(rows.map((row) => row.home?.code)).toStrictEqual(["sgp", undefined]);
    expect(rows.map((row) => row.machine.nodeId)).toStrictEqual(["1", "2"]);
  });
});

describe("labels", () => {
  it("names the NAT", () => {
    expect(natLabel(true)).toBe("Hard");
    expect(natLabel(false)).toBe("Easy");
    expect(natLabel(null)).toBe("Unknown");
  });

  it("colours the NAT", () => {
    expect(natTone(true)).toBe("warning");
    expect(natTone(false)).toBe("success");
    expect(natTone(null)).toBe("neutral");
  });

  it("answers a flag the client has not tested with Unknown", () => {
    expect(yesNo(true)).toBe("Yes");
    expect(yesNo(false)).toBe("No");
    expect(yesNo(null)).toBe("Unknown");
  });

  it("names the link", () => {
    expect(linkLabel("wifi")).toBe("Wi-Fi");
    expect(linkLabel("wired")).toBe("Wired");
    expect(linkLabel("mobile")).toBe("Mobile");
    expect(linkLabel("")).toBe("Unknown");
    expect(linkLabel("starlink")).toBe("starlink");
  });
});

describe("port mapping", () => {
  it("lists the protocols that answered", () => {
    const info = netInfo({ upnp: true, pcp: true, havePortMap: true });

    expect(portMapProtocols(info)).toStrictEqual(["UPnP", "PCP"]);
    expect(portMapLabel(info)).toBe("UPnP, PCP");
  });

  it("says a mapping is open even when no protocol is named", () => {
    expect(portMapLabel(netInfo({ havePortMap: true }))).toBe("Open");
  });

  it("separates nothing found from nothing tested", () => {
    expect(portMapLabel(netInfo({ upnp: false, pmp: false, pcp: false }))).toBe("None found");
    expect(portMapLabel(netInfo())).toBe("Unknown");
  });
});

describe("a node's own measurements", () => {
  const info = netInfo({
    preferredDerp: 2,
    latency: [
      { regionId: 1, code: "nyc", name: "New York City", ms: 40, ipv4Ms: 40, ipv6Ms: 0 },
      { regionId: 2, code: "sfo", name: "San Francisco", ms: 9, ipv4Ms: 9, ipv6Ms: 0 },
      { regionId: 3, code: "lhr", name: "London", ms: 0, ipv4Ms: 0, ipv6Ms: 0 },
      { regionId: 4, code: "ams", name: "Amsterdam", ms: 0, ipv4Ms: 0, ipv6Ms: 0 },
      { regionId: 5, code: "fra", name: "Frankfurt", ms: 21, ipv4Ms: 21, ipv6Ms: 0 },
    ],
  });

  it("finds the home region", () => {
    expect(homeLatency(info)?.code).toBe("sfo");
    expect(homeLatency(netInfo({ preferredDerp: 0, latency: info.latency }))).toBeUndefined();
  });

  it("sorts the rest nearest first and the unmeasured last", () => {
    expect(otherLatencies(info).map((entry) => entry.code)).toStrictEqual([
      "fra",
      "nyc",
      "ams",
      "lhr",
    ]);
  });

  it("leaves the source array alone", () => {
    const before = info.latency.map((entry) => entry.code);

    otherLatencies(info);

    expect(info.latency.map((entry) => entry.code)).toStrictEqual(before);
  });
});
