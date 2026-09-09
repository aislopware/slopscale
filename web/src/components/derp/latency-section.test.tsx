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

import type { DerpLatencyReport } from "~/api/schema.gen.ts";
import { LatencySection } from "~/components/derp/latency-section.tsx";

const report: DerpLatencyReport = {
  reporting: 1,
  silent: 0,
  hardNat: 0,
  regions: [
    {
      regionId: 7,
      code: "sgp",
      name: "Singapore",
      inMap: true,
      preferredBy: 1,
      samples: 1,
      minMs: 4,
      medianMs: 4,
      p90Ms: 4,
      maxMs: 4,
    },
  ],
  machines: [
    {
      nodeId: "1",
      name: "laptop",
      online: true,
      preferredDerp: 7,
      homeMs: 4,
      hardNat: false,
      linkType: "wifi",
    },
  ],
};

function page(data: DerpLatencyReport): ReactElement {
  const rootRoute = createRootRoute({ component: () => <LatencySection report={data} /> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/machines/$nodeId" }),
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

describe(LatencySection, () => {
  it("puts both lists through the shared table, with their counts", async () => {
    const screen = await render(page(report));

    // The region names its own row and the home region of the machine below it.
    await expect.element(screen.getByText("Singapore").first()).toBeVisible();
    await expect.element(screen.getByText("Showing 1 region")).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "Connected laptop" }))
      .toHaveAttribute("href", "/machines/1");
    await expect.element(screen.getByText("Showing 1 machine")).toBeVisible();
  });

  it("says so while nothing has been measured", async () => {
    const screen = await render(page({ ...report, reporting: 0, regions: [], machines: [] }));

    await expect.element(screen.getByText("No measurements yet")).toBeVisible();
    await expect.element(screen.getByText("No machines reporting")).toBeVisible();
  });
});
