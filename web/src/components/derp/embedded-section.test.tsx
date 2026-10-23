import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import type { Derp } from "~/api/queries.ts";
import type { DerpSettings } from "~/api/schema.gen.ts";
import { EmbeddedSection } from "~/components/derp/embedded-section.tsx";
import { useDerpMutations } from "~/components/derp/mutations.ts";

const settings: DerpSettings = {
  urls: [],
  regions: [{ id: 900, code: "sgp", nodes: [{ hostName: "derp.example.com" }] }],
  autoUpdate: false,
  updateFrequency: "3h0m0s",
  server: {
    enabled: true,
    regionId: 999,
    regionCode: "slopscale",
    regionName: "Embedded",
    verifyClients: true,
    stunEnabled: true,
    stunAddr: "0.0.0.0:3478",
    ipv4: "",
    ipv6: "",
  },
};

const derp: Derp = {
  autoAddEmbedded: true,
  effective: settings,
  fetchedAt: "2026-09-07T00:00:00Z",
  fetchError: "",
  fromFile: settings,
  overridden: false,
  paths: [],
  regions: [],
  relayAvailable: true,
  relayRunning: true,
  serverUrl: "https://hs.example.com",
  stunAddr: "0.0.0.0:3478",
};

function Harness(): ReactElement {
  const mutations = useDerpMutations();

  return <EmbeddedSection derp={derp} canEdit mutations={mutations} />;
}

function renderSection(): ReturnType<typeof render> {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe(EmbeddedSection, () => {
  it("turns the details card into its fields and back on Escape", async () => {
    const screen = await renderSection();

    await expect.element(screen.getByText("999 · slopscale · Embedded")).toBeVisible();
    await screen.getByRole("button", { name: "Edit details" }).click();

    const form = screen.getByRole("form", { name: "Embedded relay details" });

    await expect.element(form.getByLabelText("Region id")).toHaveValue("999");
    await expect.element(form.getByLabelText("Region id")).toHaveFocus();
    await expect.element(screen.getByRole("dialog")).not.toBeInTheDocument();

    await userEvent.keyboard("{Escape}");

    await expect.element(form).not.toBeInTheDocument();
    await expect.element(screen.getByText("999 · slopscale · Embedded")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Edit details" })).toHaveFocus();
  });

  it("says what is wrong on a save and shows the server's refusal in the card", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Edit details" }).click();

    const form = screen.getByRole("form", { name: "Embedded relay details" });

    await form.getByLabelText("Region id").fill("900");
    await screen.getByRole("button", { name: "Save" }).click();

    await expect.element(form.getByText("Another region already uses this id.")).toBeVisible();

    await form.getByLabelText("Region id").fill("998");
    // The test server has no API, so the save is refused for real.
    await screen.getByRole("button", { name: "Save" }).click();

    await expect.element(form.getByText("Not saved")).toBeVisible();
  });
});
