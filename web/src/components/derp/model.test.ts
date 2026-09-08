import { describe, expect, it } from "vitest";

import type { DerpSettings } from "~/api/schema.gen.ts";
import {
  cloneSettings,
  durationSeconds,
  frequencyError,
  frequencyLabel,
  hostNameError,
  regionIdError,
  relayDraft,
  relayError,
  relayFromDraft,
  serverDraft,
  serverError,
  serverFromDraft,
  shortDuration,
  stunAddrError,
  tailscaleMapUrl,
  withRegion,
  withUrl,
  withoutRegion,
  withoutUrl,
} from "~/components/derp/model.ts";

const base: DerpSettings = {
  urls: ["https://controlplane.tailscale.com/derpmap/default"],
  regions: [{ id: 900, code: "sgp", name: "Singapore", nodes: [{ hostName: "derp.example.com" }] }],
  autoUpdate: true,
  updateFrequency: "3h0m0s",
  server: {
    enabled: true,
    regionId: 999,
    regionCode: "headscale",
    regionName: "",
    verifyClients: true,
    stunAddr: "0.0.0.0:3478",
    ipv4: "",
    ipv6: "",
  },
};

describe("durations", () => {
  it("reads Go durations", () => {
    expect(durationSeconds("3h0m0s")).toBe(10_800);
    expect(durationSeconds("90m")).toBe(5400);
    expect(durationSeconds("1.5h")).toBe(5400);
    expect(durationSeconds("soon")).toBeNull();
    expect(durationSeconds("")).toBeNull();
  });

  it("shortens to the largest unit that fits", () => {
    expect(shortDuration("3h0m0s")).toBe("3h");
    expect(shortDuration("24h0m0s")).toBe("1d");
    expect(shortDuration("90m")).toBe("90m");
    expect(shortDuration("45s")).toBe("45s");
    expect(shortDuration("nope")).toBe("nope");
  });

  it("labels the presets", () => {
    expect(frequencyLabel("1h")).toBe("Every hour");
    expect(frequencyLabel("3h0m0s")).toBe("Every 3 hours");
    expect(frequencyLabel("15m")).toBe("Every 15 minutes");
    expect(frequencyLabel("24h")).toBe("Every day");
  });

  it("refuses anything under a minute", () => {
    expect(frequencyError("30s")).not.toBeNull();
    expect(frequencyError("1m")).toBeNull();
    expect(frequencyError("later")).not.toBeNull();
  });
});

describe("edits", () => {
  it("copies without sharing the regions", () => {
    const copy = cloneSettings(base);

    copy.regions?.[0]?.nodes?.push({ hostName: "other.example.com" });

    expect(copy.regions?.[0]?.nodes).toHaveLength(2);
    expect(base.regions[0]?.nodes).toHaveLength(1);
  });

  it("adds a URL once", () => {
    const url = "https://maps.example.com/derp.json";

    expect(withUrl(base, url).urls).toStrictEqual([...base.urls, url]);
    expect(withUrl({ ...base, urls: [url] }, url).urls).toStrictEqual([url]);
  });

  it("removes a URL", () => {
    expect(withoutUrl(base, tailscaleMapUrl).urls).toStrictEqual([]);
  });

  it("replaces a region by its previous id and keeps the list sorted", () => {
    const moved = withRegion(base, { id: 901, code: "sgp", nodes: [] }, 900);
    const added = withRegion(base, { id: 800, code: "fra", nodes: [] });

    expect(moved.regions?.map((region) => region.id)).toStrictEqual([901]);
    expect(added.regions?.map((region) => region.id)).toStrictEqual([800, 900]);
    expect(withoutRegion(base, 900).regions).toStrictEqual([]);
  });
});

describe("validation", () => {
  it("checks region ids against the ones in use", () => {
    expect(regionIdError("900", [900])).not.toBeNull();
    expect(regionIdError("0", [])).not.toBeNull();
    expect(regionIdError("abc", [])).not.toBeNull();
    expect(regionIdError("901", [900])).toBeNull();
  });

  it("checks host names", () => {
    expect(hostNameError("derp.example.com")).toBeNull();
    expect(hostNameError("not a host")).not.toBeNull();
  });

  it("checks STUN addresses", () => {
    expect(stunAddrError("0.0.0.0:3478")).toBeNull();
    expect(stunAddrError("[::]:3478")).toBeNull();
    expect(stunAddrError("3478")).not.toBeNull();
    expect(stunAddrError("0.0.0.0:70000")).not.toBeNull();
  });

  it("round-trips a relay through its draft", () => {
    const relay = { hostName: "derp.example.com", ipv4: "none", derpPort: 8443, stunOnly: true };
    const draft = relayDraft(relay);

    expect(relayError(draft)).toBeNull();
    expect(relayFromDraft(draft)).toStrictEqual(relay);
    expect(relayError({ ...draft, ipv4: "bad" })).not.toBeNull();
    expect(relayFromDraft(relayDraft())).toStrictEqual({ hostName: "" });
  });

  it("round-trips the embedded relay through its draft", () => {
    const draft = serverDraft(base.server);

    expect(serverError(draft, [900])).toBeNull();
    expect(serverError({ ...draft, regionId: "900" }, [900])).not.toBeNull();
    expect(serverFromDraft(draft, false)).toStrictEqual({ ...base.server, enabled: false });
  });
});
