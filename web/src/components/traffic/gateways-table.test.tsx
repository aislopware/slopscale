import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { GatewaysTable, gatewayState } from "~/components/traffic/gateways-table.tsx";
import { ResolverNotices } from "~/components/traffic/resolver-notices.tsx";

const now = new Date().toISOString();

const office: TrafficReporter = {
  collectors: {
    conntrack: { enabled: true, error: "" },
    sni: { enabled: true, error: "" },
    dns: { enabled: true, error: "" },
    appConnector: { enabled: false, error: "" },
  },
  dnsListen: ["100.64.0.8:53"],
  dropped: 0,
  firstSeenAt: now,
  instance: "a1",
  lastReportAt: now,
  nodeId: "8",
  nodeName: "office-gateway",
  online: true,
  refused: "",
  resolverActive: false,
  stale: false,
  unattributed: 0,
  version: "0.47.0",
};

const branch: TrafficReporter = {
  ...office,
  nodeId: "10",
  nodeName: "branch-gateway",
  dnsListen: [],
  collectors: { ...office.collectors, dns: { enabled: false, error: "" } },
  refused: "the device is not tagged; only tagged gateways may report traffic",
};

/** The gateway cells link into the app and the menu mutates, so both a router and a query client. */
function app(body: ReactNode): ReactElement {
  const rootRoute = createRootRoute({ component: () => body });
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

describe(GatewaysTable, () => {
  it("says why a gateway is refused and whether its resolver waits for an approval", async () => {
    const screen = await render(
      app(<GatewaysTable reporters={[office, branch]} writable canApprove />),
    );

    await expect.element(screen.getByText("Refused")).toBeVisible();
    await expect
      .element(
        screen.getByText("The device is not tagged; only tagged gateways may report traffic."),
      )
      .toBeVisible();
    await expect.element(screen.getByText("Not approved")).toBeVisible();
    expect(gatewayState(branch)).toBe("refused");
  });

  it("shows an approved resolver in use with when it was approved", async () => {
    const approved = { ...office, resolverActive: true, resolverApprovedAt: now };
    const screen = await render(app(<GatewaysTable reporters={[approved]} writable canApprove />));

    await expect.element(screen.getByText("100.64.0.8:53")).toBeVisible();
    await expect.element(screen.getByText(/^Approved/u)).toBeVisible();
  });

  it("asks before handing the clients' DNS to a gateway, and only with the DNS permission", async () => {
    const screen = await render(app(<GatewaysTable reporters={[office]} writable canApprove />));

    await screen.getByRole("button", { name: "Actions for gateway office-gateway" }).click();
    await screen.getByRole("menuitem", { name: "Use its resolver for DNS…" }).click();

    const dialog = screen.getByRole("alertdialog");

    await expect.element(dialog.getByText(/one approved gateway resolver/u)).toBeVisible();
    await expect.element(dialog.getByRole("button", { name: "Approve resolver" })).toBeVisible();
  });

  it("holds the approval back from a caller without the DNS permission", async () => {
    const screen = await render(
      app(<GatewaysTable reporters={[office]} writable canApprove={false} />),
    );

    await screen.getByRole("button", { name: "Actions for gateway office-gateway" }).click();

    await expect
      .element(screen.getByRole("menuitem", { name: "Use its resolver for DNS…" }))
      .toHaveAttribute("aria-disabled", "true");
  });
});

describe(ResolverNotices, () => {
  it("says why DNS logging reaches no machine and which nameservers the resolvers skip", async () => {
    const screen = await render(
      app(
        <ResolverNotices
          reporters={{
            dnsBlocked: "DNS logging needs at least one global nameserver",
            skippedUpstreams: ["tls://dns.example", "100.64.0.53"],
          }}
        />,
      ),
    );

    await expect
      .element(screen.getByText("DNS logging points no machine at a gateway resolver"))
      .toBeVisible();
    await expect.element(screen.getByText(/tls:\/\/dns\.example, 100\.64\.0\.53/u)).toBeVisible();
  });

  it("stays away while the resolvers have what they need", async () => {
    const screen = await render(
      app(<ResolverNotices reporters={{ dnsBlocked: "", skippedUpstreams: [] }} />),
    );

    expect(screen.container.textContent).toBe("");
  });
});
