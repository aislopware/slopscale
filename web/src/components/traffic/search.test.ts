import { object, parse } from "valibot";
import { describe, expect, it, vi } from "vitest";

import type { TrafficDestination } from "~/api/traffic.ts";
import { trafficWindow } from "~/components/traffic/range.ts";
import {
  destinationChips,
  destinationSearchEntries,
  noDestinationFilters,
  pickDestination,
} from "~/components/traffic/search.ts";
import type { DestinationSearch } from "~/components/traffic/search.ts";

const search = object(destinationSearchEntries);

const row: TrafficDestination = {
  asn: 15_169,
  asName: "Google LLC",
  conns: 3,
  country: "US",
  dst: "142.250.1.1",
  host: "www.youtube.com",
  nodeId: "7",
  nodeName: "laptop",
  nodes: 2,
  port: 443,
  private: false,
  proto: 6,
  rxBytes: 100,
  rxPackets: 1,
  txBytes: 10,
  txPackets: 1,
};

describe("the destinations address", () => {
  it("reads a bare address as the last day grouped by host", () => {
    expect(parse(search, {})).toStrictEqual({
      range: "24h",
      from: "",
      to: "",
      gateway: "",
      by: "host",
      ...noDestinationFilters,
    });
  });

  it("keeps an id the router read as a number, and drops what it cannot use", () => {
    const read = parse(search, { gateway: 2, node: 7, by: "nonsense", asn: -3, range: "2y" });

    expect(read).toMatchObject({ gateway: "2", node: "7", by: "host", asn: 0, range: "24h" });
  });
});

describe(trafficWindow, () => {
  const now = new Date("2026-09-25T10:30:45Z");
  const base = { from: "", to: "", gateway: "" };

  it("ends a preset on the last whole minute", () => {
    expect(trafficWindow({ ...base, range: "1h" }, now)).toStrictEqual({
      start: "2026-09-25T09:30:00.000Z",
      end: "2026-09-25T10:30:00.000Z",
    });
  });

  it("takes a custom range as given and falls back to the last day when it is backwards", () => {
    const custom = { ...base, range: "custom", from: "2026-09-01T00:00:00Z" } as const;

    expect(trafficWindow({ ...custom, to: "2026-09-02T00:00:00Z" }, now)).toStrictEqual({
      start: "2026-09-01T00:00:00.000Z",
      end: "2026-09-02T00:00:00.000Z",
    });
    expect(trafficWindow({ ...custom, to: "2026-08-01T00:00:00Z" }, now).start).toBe(
      "2026-09-24T10:30:00.000Z",
    );
  });
});

describe(pickDestination, () => {
  it("turns a place into the machines that reached it, and a machine into its hosts", () => {
    expect(pickDestination(row, "host")).toStrictEqual({ q: "www.youtube.com", by: "node" });
    expect(pickDestination(row, "asn")).toStrictEqual({ asn: 15_169, by: "node" });
    expect(pickDestination(row, "port")).toStrictEqual({ proto: 6, port: 443, by: "node" });
    expect(pickDestination(row, "reporter")).toStrictEqual({ gateway: "7", by: "host" });
    expect(pickDestination(row, "node")).toStrictEqual({ node: "7", by: "host" });
  });
});

describe(destinationChips, () => {
  it("spells out each narrowing and clears only its own field", () => {
    const narrowed: DestinationSearch = {
      ...parse(search, {}),
      by: "node",
      country: "VN",
      proto: 6,
      port: 443,
      node: "7",
    };
    const onChange = vi.fn<(next: DestinationSearch) => void>();
    const chips = destinationChips(narrowed, (id) => `machine ${id}`, onChange);

    expect(chips.map((chip) => chip.name)).toStrictEqual(["Country", "Port", "Machine"]);
    expect(chips.find((chip) => chip.name === "Machine")?.value).toBe("machine 7");

    chips.find((chip) => chip.name === "Port")?.onRemove();

    expect(onChange).toHaveBeenCalledWith({ ...narrowed, proto: 0, port: 0 });
  });
});
