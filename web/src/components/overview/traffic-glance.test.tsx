import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { TrafficDestination, TrafficNode, TrafficSummary } from "~/api/traffic.ts";
import { app } from "~/components/overview/overview-fixtures.tsx";
import { TrafficGlance } from "~/components/overview/traffic-glance.tsx";
import { emptySummary } from "~/components/traffic/summary.tsx";

const gib = 1024 ** 3;

function machine(nodeId: string, nodeName: string, bytes: number): TrafficNode {
  return {
    nodeId,
    nodeName,
    reporterIds: [],
    rxBytes: bytes,
    txBytes: 0,
    rxPackets: 0,
    txPackets: 0,
    conns: 1,
    nodeOwner: {
      tags: [],
      userId: "1",
      userName: "alice",
      displayName: "Alice",
      profilePicUrl: "",
    },
  };
}

function host(name: string, bytes: number, nodes: number): TrafficDestination {
  return {
    asn: 0,
    asName: "",
    conns: 1,
    country: "",
    dst: name === "" ? "" : "142.250.1.1",
    host: name,
    nodeId: "",
    nodeName: "",
    nodes,
    port: 443,
    private: false,
    proto: 6,
    rxBytes: bytes,
    rxPackets: 0,
    txBytes: 0,
    txPackets: 0,
  };
}

const machines = [
  machine("1", "laptop", 3 * gib),
  machine("2", "build", 2 * gib),
  machine("3", "nas", gib),
  machine("4", "phone", gib),
  machine("5", "tv", gib),
  machine("6", "printer", gib),
];

const summary: TrafficSummary = {
  ...emptySummary,
  start: "2026-09-26T10:00:00Z",
  end: "2026-09-27T10:00:00Z",
  resolution: 3600,
  total: { conns: 12, rxBytes: 9 * gib, rxPackets: 0, txBytes: 0, txPackets: 0 },
  nodes: machines,
};

const noZoom = (): void => undefined;

describe(TrafficGlance, () => {
  it("shows the day's totals and the five busiest hosts and machines, each linking on", async () => {
    const screen = await render(
      app(
        <TrafficGlance
          summary={summary}
          destinations={[host("github.com", 4 * gib, 3), host("", gib, 2)]}
          error={null}
          onZoom={noZoom}
        />,
      ),
    );

    await expect.element(screen.getByText("9.0 GiB")).toBeVisible();
    await expect.element(screen.getByText("3 machines")).toBeVisible();
    await expect.element(screen.getByText("tv")).toBeVisible();
    await expect.element(screen.getByText("printer")).not.toBeInTheDocument();

    const laptop = screen.getByRole("link", { name: /laptop/u });

    await expect
      .element(laptop)
      .toHaveAttribute("href", expect.stringMatching(/^\/traffic\/machines\/1\?/u));

    const github = screen.getByRole("link", { name: /github\.com/u });

    await expect
      .element(github)
      .toHaveAttribute("href", expect.stringMatching(/host=github\.com/u));
    await expect
      .element(screen.getByText("Everything else"), { message: "the remainder is not a link" })
      .toBeVisible();
    expect(screen.getByRole("link", { name: /Everything else/u }).elements()).toHaveLength(0);
  });

  it("says so when nothing went through the gateways", async () => {
    const screen = await render(
      app(
        <TrafficGlance
          summary={{ ...emptySummary, start: summary.start, end: summary.end }}
          destinations={[]}
          error={null}
          onZoom={noZoom}
        />,
      ),
    );

    await expect.element(screen.getByText("Nothing in the last 24 hours").first()).toBeVisible();
    expect(screen.getByText("Nothing in the last 24 hours").elements()).toHaveLength(2);
  });

  it("names the failure instead of drawing an empty chart", async () => {
    const screen = await render(
      app(
        <TrafficGlance
          summary={undefined}
          destinations={undefined}
          error={new Error("the traffic store is unavailable")}
          onZoom={noZoom}
        />,
      ),
    );

    await expect.element(screen.getByText("Traffic could not be read")).toBeVisible();
    await expect.element(screen.getByText("the traffic store is unavailable")).toBeVisible();
    expect(screen.getByText("Top machines").elements()).toHaveLength(0);
  });
});
