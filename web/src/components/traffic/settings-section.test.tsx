import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { ApiError } from "~/api/error.ts";
import type { TrafficReporters, TrafficSettings } from "~/api/traffic.ts";
import { TrafficSettingsSections, retentionError } from "~/components/traffic/settings-section.tsx";
import { refusalOf } from "~/components/traffic/window-refusal.tsx";

const stored = { minuteHours: "48", hourDays: "30", dayDays: "400" };

const settings: TrafficSettings = {
  sni: true,
  dnsLogging: false,
  retention: { minuteHours: 48, hourDays: 30, dayDays: 400 },
};

const reporters: TrafficReporters = {
  asnRanges: 0,
  reporters: [],
  resolvers: [],
  skippedUpstreams: [],
};

function app(canEditDns: boolean): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <TrafficSettingsSections
        settings={settings}
        reporters={reporters}
        canEdit
        canEditDns={canEditDns}
      />
    ),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/" });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });

  return (
    <QueryClientProvider client={new QueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(TrafficSettingsSections, () => {
  it("explains that DNS logging covers only the machines using a gateway as their exit node", async () => {
    const screen = await render(app(true));

    await expect
      .element(
        screen.getByText(
          /Only machines using a gateway as their exit node are logged, and only while/u,
        ),
      )
      .toBeVisible();
    await expect.element(screen.getByText(/older than 1\.86/u)).toBeVisible();
    await expect.element(screen.getByText(/takes the DNS permission/u)).not.toBeInTheDocument();
  });

  it("says a caller without the DNS permission cannot switch it", async () => {
    const screen = await render(app(false));

    await expect.element(screen.getByText(/takes the DNS permission/u)).toBeVisible();
    await expect.element(screen.getByRole("switch", { name: "Log DNS lookups" })).toBeDisabled();
  });
});

describe(refusalOf, () => {
  it("gives the server's reason rather than the operation it failed at", () => {
    const refusal = new ApiError(
      400,
      {
        type: "about:blank",
        title: "Bad Request",
        detail: "setting traffic settings",
        errors: [{ message: "granting the traffic resolvers: policy does not compile" }],
      },
      "",
    );

    expect(refusalOf(refusal)).toBe("Granting the traffic resolvers: policy does not compile.");
    expect(refusalOf(new ApiError(500, undefined, "The server failed."))).toBe(
      "The server failed.",
    );
  });
});

describe(retentionError, () => {
  it("lets the defaults through", () => {
    expect(retentionError(stored)).toBeNull();
  });

  it("holds each field to what the server accepts", () => {
    expect(retentionError({ ...stored, minuteHours: "" })).toBe(
      "Keep per-minute totals for 1 to 168 hours.",
    );
    expect(retentionError({ ...stored, hourDays: "1.5" })).toBe(
      "Keep hourly data for 1 to 90 days.",
    );
    expect(retentionError({ ...stored, dayDays: "4000" })).toBe(
      "Keep daily data for 1 to 3650 days.",
    );
  });

  it("refuses a finer resolution that outlives a coarser one", () => {
    expect(retentionError({ ...stored, minuteHours: "72", hourDays: "2" })).toBe(
      "Per-minute totals cannot outlive the hourly data.",
    );
    expect(retentionError({ ...stored, hourDays: "60", dayDays: "30" })).toBe(
      "Hourly data cannot outlive the daily data.",
    );
  });
});
