import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { defaultTrafficWindow } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import { WindowToolbar, pickedFilter } from "~/components/traffic/window-toolbar.tsx";

function reporter(nodeId: string, nodeName: string): TrafficReporter {
  return {
    collectors: {
      conntrack: { enabled: true, error: "" },
      sni: { enabled: true, error: "" },
      dns: { enabled: true, error: "" },
      appConnector: { enabled: false, error: "" },
    },
    dnsListen: [],
    dropped: 0,
    firstSeenAt: "2026-10-01T00:00:00Z",
    instance: nodeId,
    lastReportAt: "2026-10-20T00:00:00Z",
    nodeId,
    nodeName,
    nodeOwner: {
      tags: ["tag:gateway"],
      userId: "",
      userName: "",
      displayName: "",
      profilePicUrl: "",
    },
    online: true,
    refused: "",
    resolverActive: false,
    stale: false,
    unattributed: 0,
    version: "0.47.0",
  };
}

function noop(): void {
  // The picked filter's × is not what this test presses.
}

describe(WindowToolbar, () => {
  it("names the window on the time range button and hands back a preset picked", async () => {
    const onChange = vi.fn<(next: TrafficWindowSearch) => void>();
    const screen = await render(
      <WindowToolbar search={defaultTrafficWindow} reporters={[]} onChange={onChange} />,
    );

    await screen.getByRole("button", { name: "Time range: Last 24 hours" }).click();
    await screen.getByRole("button", { name: "Last 7 days" }).click();

    expect(onChange).toHaveBeenCalledWith({ ...defaultTrafficWindow, range: "7d" });
  });

  it("names a custom range by its ends", async () => {
    const screen = await render(
      <WindowToolbar
        search={{
          ...defaultTrafficWindow,
          range: "custom",
          from: "2026-10-01T00:00:00Z",
          to: "2026-10-02T00:00:00Z",
        }}
        reporters={[]}
        onChange={vi.fn<(next: TrafficWindowSearch) => void>()}
      />,
    );

    await expect
      .element(screen.getByRole("button", { name: /^Time range: .+ → .+$/u }))
      .toBeVisible();
  });

  it("keeps the network in the Filters popover and counts it only off the internet", async () => {
    const screen = await render(
      <WindowToolbar
        search={defaultTrafficWindow}
        reporters={[]}
        onChange={vi.fn<(next: TrafficWindowSearch) => void>()}
      />,
    );

    await expect
      .element(screen.getByRole("button", { name: "Filters", exact: true }))
      .toBeVisible();
    await expect
      .element(screen.getByRole("button", { name: "Clear filters" }))
      .not.toBeInTheDocument();
    await screen.getByRole("button", { name: "Filters", exact: true }).click();
    await expect
      .element(screen.getByRole("combobox", { name: "Network", exact: true }))
      .toBeVisible();
    // The network is the scope every read has, so it has no × at its default.
    await expect
      .element(screen.getByRole("button", { name: "Remove the network filter" }))
      .not.toBeInTheDocument();
  });

  it("leaves the network out of a page whose reads do not split by it", async () => {
    const screen = await render(
      <WindowToolbar
        search={defaultTrafficWindow}
        reporters={[]}
        networks={false}
        onChange={vi.fn<(next: TrafficWindowSearch) => void>()}
      />,
    );

    await screen.getByRole("button", { name: "Filters", exact: true }).click();
    await expect.element(screen.getByText("No filters. Add one to narrow the list.")).toBeVisible();
    expect(screen.getByRole("combobox", { name: "Network", exact: true }).query()).toBeNull();
  });

  it("counts the network, the gateway and a picked filter, and clears them in one step", async () => {
    const onChange = vi.fn<(next: TrafficWindowSearch) => void>();
    const onClearFilters = vi.fn<(window: TrafficWindowSearch) => void>();
    const search = { ...defaultTrafficWindow, network: "lan", gateway: "4" } as const;
    const screen = await render(
      <WindowToolbar
        search={search}
        reporters={[reporter("4", "edge"), reporter("5", "core")]}
        onChange={onChange}
        filters={[pickedFilter({ name: "Host", value: "example.com", onRemove: noop })]}
        onClearFilters={onClearFilters}
      />,
    );

    await expect.element(screen.getByRole("button", { name: "Filters, 3 applied" })).toBeVisible();
    await screen.getByRole("button", { name: "Clear filters" }).click();

    expect(onClearFilters).toHaveBeenCalledWith({ ...search, network: "internet", gateway: "" });
    expect(onChange).not.toHaveBeenCalled();
  });

  it("drops one filter with its own ×", async () => {
    const onChange = vi.fn<(next: TrafficWindowSearch) => void>();
    const search = { ...defaultTrafficWindow, gateway: "4" };
    const screen = await render(
      <WindowToolbar
        search={search}
        reporters={[reporter("4", "edge"), reporter("5", "core")]}
        onChange={onChange}
      />,
    );

    await screen.getByRole("button", { name: "Filters, 1 applied" }).click();
    await screen.getByRole("button", { name: "Remove the gateway filter" }).click();

    expect(onChange).toHaveBeenCalledWith({ ...search, gateway: "" });
  });
});
