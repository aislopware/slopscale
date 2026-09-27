import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { defaultTrafficWindow } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";

describe(WindowToolbar, () => {
  it("opens on the internet and hands back the network picked", async () => {
    const onChange = vi.fn<(next: TrafficWindowSearch) => void>();
    const screen = await render(
      <WindowToolbar search={defaultTrafficWindow} reporters={[]} onChange={onChange} />,
    );

    await expect
      .element(screen.getByRole("tab", { name: "Internet" }))
      .toHaveAttribute("aria-selected", "true");

    await screen.getByRole("tab", { name: "LAN" }).click();

    expect(onChange).toHaveBeenCalledWith({ ...defaultTrafficWindow, network: "lan" });
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

    await expect.element(screen.getByRole("tab", { name: "24h" })).toBeVisible();
    expect(screen.getByRole("tablist", { name: "Network" }).query()).toBeNull();
  });
});
