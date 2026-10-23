import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Derp } from "~/api/queries.ts";
import type { DerpCustomRegion, DerpSettings } from "~/api/schema.gen.ts";
import { RegionEditor } from "~/components/derp/region-editor.tsx";

const region: DerpCustomRegion = {
  id: 900,
  code: "sgp",
  name: "Singapore",
  nodes: [{ hostName: "derp1.example.com" }, { hostName: "derp2.example.com", stunPort: 3478 }],
};

const settings: DerpSettings = {
  urls: [],
  regions: [region],
  autoUpdate: false,
  updateFrequency: "3h0m0s",
  server: { enabled: true, regionId: 999, regionCode: "slopscale" },
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

function app(editing?: DerpCustomRegion): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <RegionEditor region={editing} derp={derp} canEdit />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/relays/own" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(RegionEditor, () => {
  it("says what is missing when an empty region is saved", async () => {
    const screen = await render(app());

    await screen.getByRole("button", { name: "Add region" }).click();

    await expect.element(screen.getByText("Use a positive number.")).toBeVisible();
    await expect.element(screen.getByText("Enter a code, such as sgp.")).toBeVisible();
    await expect.element(screen.getByText("Enter the relay's host name.")).toBeVisible();
    await expect.element(screen.getByLabelText("Region id")).toHaveFocus();

    // A relay added after the save starts clean.
    await screen.getByRole("button", { name: "Add relay" }).click();

    await expect.element(screen.getByText("Enter the relay's host name.")).toBeVisible();
    await expect
      .element(
        screen.getByRole("group", { name: "Relay 2" }).getByText("Enter the relay's host name."),
      )
      .not.toBeInTheDocument();
  });

  it("refuses an id another region or the embedded relay uses, once the field is left", async () => {
    const screen = await render(app());

    await screen.getByLabelText("Region id").fill("999");
    await screen.getByLabelText("Code").click();

    await expect.element(screen.getByText("Another region already uses this id.")).toBeVisible();
  });

  it("adds and removes relays as cards of their own", async () => {
    const screen = await render(app(region));

    await expect.element(screen.getByRole("group", { name: "Relay 2" })).toBeVisible();
    await expect
      .element(
        screen
          .getByRole("group", { name: "Relay 2" })
          .getByLabelText("STUN port", { exact: false }),
      )
      .toHaveValue("3478");

    await screen.getByRole("button", { name: "Remove relay 1" }).click();

    await expect.element(screen.getByRole("group", { name: "Relay 2" })).not.toBeInTheDocument();
    await expect
      .element(screen.getByRole("group", { name: "Relay 1" }).getByLabelText("Host name"))
      .toHaveValue("derp2.example.com");
    // The last relay cannot go: a region needs one.
    await expect
      .element(screen.getByRole("button", { name: "Remove relay 1" }))
      .not.toBeInTheDocument();

    await screen.getByRole("button", { name: "Add relay" }).click();

    await expect.element(screen.getByRole("group", { name: "Relay 2" })).toBeVisible();
  });

  it("shows the server's refusal over the form", async () => {
    const screen = await render(app(region));

    // The test server has no API, so the save is refused for real.
    await screen.getByRole("button", { name: "Save region" }).click();

    await expect.element(screen.getByText("The region was not saved")).toBeVisible();
  });
});
