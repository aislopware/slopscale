import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import { ApiError } from "~/api/error.ts";
import type { TrafficDestination } from "~/api/traffic.ts";
import { DestinationsTable } from "~/components/traffic/destinations-table.tsx";
import { WindowRefusal, loadWindow } from "~/components/traffic/window-refusal.tsx";

const blank: TrafficDestination = {
  asn: 0,
  asName: "",
  conns: 1,
  country: "",
  dst: "",
  host: "",
  nodeId: "",
  nodeName: "",
  nodes: 1,
  port: 0,
  private: false,
  proto: 0,
  rxBytes: 0,
  rxPackets: 0,
  txBytes: 0,
  txPackets: 0,
};

const youtube: TrafficDestination = {
  ...blank,
  host: "www.youtube.com",
  asn: 15_169,
  asName: "Google LLC",
  country: "US",
  nodes: 3,
  rxBytes: 3 * 1024 ** 3,
  txBytes: 1024 ** 2,
};

const nas: TrafficDestination = { ...blank, host: "nas.office.lan", private: true, rxBytes: 2048 };

/** What a busy hour's smaller destinations are folded into. */
const remainder: TrafficDestination = { ...blank, rxBytes: 1024 };

describe(DestinationsTable, () => {
  it("lists hosts busiest first, marks a LAN one, and picks a row but not the remainder", async () => {
    const onPick = vi.fn<(row: TrafficDestination) => void>();
    const screen = await render(
      <DestinationsTable
        rows={[nas, remainder, youtube]}
        groupBy="host"
        whole={youtube.rxBytes + youtube.txBytes + nas.rxBytes + remainder.rxBytes}
        onPick={onPick}
      />,
    );
    await expect.element(screen.getByText("Everything else")).toBeVisible();

    const rows = screen
      .getByRole("row")
      .elements()
      .map((row) => row.textContent);

    expect(rows[1]).toMatch(/^www\.youtube\.com3.*3\.0 GiB/u);
    expect(rows[2]).toMatch(/^nas\.office\.lanLAN/u);
    expect(rows[3]).toMatch(/^Everything else/u);

    await screen.getByText("Everything else").click();

    expect(onPick).not.toHaveBeenCalled();

    await screen.getByText("www.youtube.com").click();

    expect(onPick).toHaveBeenCalledWith(youtube);
  });

  it("shows a network by name with its number, and an address's host under it", async () => {
    const screen = await render(
      <DestinationsTable
        rows={[{ ...youtube, dst: "142.250.1.1", proto: 6, port: 443 }]}
        groupBy="destination"
        whole={youtube.rxBytes}
      />,
    );

    await expect.element(screen.getByText("142.250.1.1")).toBeVisible();
    await expect
      .element(screen.getByTitle("AS15169 Google LLC"))
      .toHaveTextContent("Google LLCAS15169");
    await expect.element(screen.getByTitle("www.youtube.com")).toBeVisible();
  });
});

describe("a private group", () => {
  it("reads as LAN in the network and country views, not as unknown", async () => {
    const lan: TrafficDestination = { ...blank, private: true, rxBytes: 10 };
    const unknown: TrafficDestination = { ...blank, rxBytes: 5 };
    const networks = await render(
      <DestinationsTable rows={[lan]} groupBy="asn" whole={lan.rxBytes} />,
    );

    await expect.element(networks.getByText("LAN")).toBeVisible();
    await networks.unmount();

    const countries = await render(
      <DestinationsTable rows={[{ ...unknown, private: false }]} groupBy="country" whole={5} />,
    );

    await expect.element(countries.getByText("Unknown")).toBeVisible();
  });
});

describe(WindowRefusal, () => {
  it("shows the server's reason for a refused window and offers the default one", async () => {
    const onReset = vi.fn<() => void>();
    const refusal = new ApiError(
      400,
      { type: "about:blank", title: "Bad Request", detail: "a window may span at most 400 days" },
      "",
    );
    const screen = await render(<WindowRefusal failure={refusal} onReset={onReset} />);

    await expect.element(screen.getByText("a window may span at most 400 days")).toBeVisible();
    await screen.getByRole("button", { name: "Last 24 hours" }).click();

    expect(onReset).toHaveBeenCalledOnce();
  });

  it("stays out of the way of any other failure, which the loader passes on", async () => {
    const forbidden = new ApiError(403, undefined, "Forbidden");
    const refused = new ApiError(400, undefined, "no");
    const screen = await render(
      <WindowRefusal failure={forbidden} onReset={vi.fn<() => void>()} />,
    );

    expect(screen.container.textContent).toBe("");
    await expect(loadWindow([Promise.reject(forbidden)])).rejects.toBe(forbidden);
    await expect(loadWindow([Promise.reject(refused)])).resolves.toBeUndefined();
  });
});
