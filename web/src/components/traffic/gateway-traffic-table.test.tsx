import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { TrafficNode } from "~/api/traffic.ts";
import { GatewayTrafficTable, gatewayRows } from "~/components/traffic/gateway-traffic-table.tsx";
import { MachinesTable } from "~/components/traffic/machines-table.tsx";

const window = { range: "24h", from: "", to: "", gateway: "", network: "internet" } as const;

const gatewayOwner = {
  tags: ["tag:gateway"],
  userId: "",
  userName: "",
  displayName: "",
  profilePicUrl: "",
};

function node({
  nodeId,
  nodeName,
  bytes,
  reporterIds = [],
}: {
  nodeId: string;
  nodeName: string;
  bytes: number;
  reporterIds?: string[];
}): TrafficNode {
  return {
    nodeId,
    nodeName,
    reporterIds,
    rxBytes: bytes,
    txBytes: 0,
    rxPackets: 0,
    txPackets: 0,
    conns: 1,
  };
}

const office = {
  ...node({ nodeId: "1", nodeName: "office-1", bytes: 3 * 1024 ** 3 }),
  nodeOwner: gatewayOwner,
};
const dcOld = {
  ...node({ nodeId: "2", nodeName: "dc-old", bytes: 5 * 1024 ** 2 }),
  nodeOwner: gatewayOwner,
};

/** A gateway that reports but carried nothing in the window. */
const eu = { nodeId: "3", nodeName: "eu", nodeOwner: gatewayOwner };

/** The machines table links into the app, so it needs a router. */
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

function rowTexts(screen: Awaited<ReturnType<typeof render>>): (string | null)[] {
  return screen
    .getByRole("row")
    .elements()
    .map((row) => row.textContent);
}

describe(gatewayRows, () => {
  it("keeps the gateways that carried traffic and adds the quiet ones at zero", () => {
    const rows = gatewayRows([office], [office, eu]);

    expect(rows.map((row) => [row.nodeName, row.rxBytes + row.txBytes])).toStrictEqual([
      ["office-1", office.rxBytes],
      ["eu", 0],
    ]);
    expect(rows[1]?.nodeOwner).toStrictEqual(gatewayOwner);
  });
});

describe(GatewayTrafficTable, () => {
  it("lists every gateway busiest first, a quiet one at zero, and picks a row", async () => {
    const onPick = vi.fn<(gateway: string) => void>();
    const gateways = gatewayRows([office, dcOld], [eu]);
    const screen = await render(
      <GatewayTrafficTable
        gateways={gateways}
        whole={office.rxBytes + dcOld.rxBytes}
        onPick={onPick}
      />,
    );

    await expect.element(screen.getByText("eu", { exact: true })).toBeVisible();

    const rows = rowTexts(screen);

    expect(rows[1]).toMatch(/^office-1.*3\.0 GiB/u);
    expect(rows[2]).toMatch(/^dc-old.*5\.0 MiB/u);
    expect(rows[3]).toMatch(/^eu.*0 B/u);

    await screen.getByText("dc-old").click();

    expect(onPick).toHaveBeenCalledWith("2");
  });
});

describe(MachinesTable, () => {
  const laptop = node({ nodeId: "7", nodeName: "laptop", bytes: 2048, reporterIds: ["1", "2"] });
  const phone = node({ nodeId: "8", nodeName: "phone", bytes: 1024, reporterIds: ["2", "9"] });

  it("names each machine's gateways, busiest first, when several carried traffic", async () => {
    const screen = await render(
      app(
        <MachinesTable
          nodes={[laptop, phone]}
          gateways={[office, dcOld]}
          whole={3072}
          search={window}
        />,
      ),
    );

    await expect.element(screen.getByText("Gateway 9")).toBeVisible();

    const rows = rowTexts(screen);

    expect(rows[0]).toContain("Gateways");
    expect(rows[1]).toMatch(/^laptop.*office-1dc-old/u);
    // Gateway 9 is not among the window's gateways, so it keeps its id.
    expect(rows[2]).toMatch(/^phone.*dc-oldGateway 9/u);
  });

  it("leaves the column out when one gateway carried everything", async () => {
    const screen = await render(
      app(
        <MachinesTable
          nodes={[{ ...laptop, reporterIds: ["1"] }]}
          gateways={[office]}
          whole={2048}
          search={window}
        />,
      ),
    );

    await expect.element(screen.getByText("laptop")).toBeVisible();

    expect(rowTexts(screen)[0]).not.toContain("Gateways");
  });
});
