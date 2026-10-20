import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Service } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { ServicesTab } from "~/components/services/services-tab.tsx";

const stamp = "2026-01-01T12:00:00Z";

const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scoped: false,
  scopes: [],
  permissions: { services: true },
};

function service(label: string, extra: Partial<Service> = {}): Service {
  return {
    id: label,
    name: `svc:${label}`,
    displayName: "",
    dnsName: `${label}.example.ts.net`,
    addresses: ["100.64.0.10"],
    ports: [],
    comment: "",
    hosts: [],
    createdAt: stamp,
    updatedAt: stamp,
    ...extra,
  };
}

const services: readonly Service[] = [
  service("web", { displayName: "Company wiki", ports: ["tcp:443"] }),
  service("grafana", { comment: "Metrics for the ops team" }),
];

function Page({ list }: { readonly list: readonly Service[] }): ReactElement {
  const [search, setSearch] = useState("");

  return <ServicesTab me={operator} services={list} search={search} onSearchChange={setSearch} />;
}

function page(list: readonly Service[] = services): ReactElement {
  const rootRoute = createRootRoute({ component: () => <Page list={list} /> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/services/$label" }),
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

describe(ServicesTab, () => {
  it("shows a card per service, named by its display name and linked to its page", async () => {
    const screen = await render(page());
    const cards = screen.getByRole("list", { name: "Services" }).getByRole("listitem");

    await expect.element(cards.nth(0).getByRole("link", { name: "svc:grafana" })).toBeVisible();
    await expect
      .element(cards.nth(1).getByRole("link", { name: "Company wiki" }))
      .toHaveAttribute("href", "/services/web");
    await expect.element(screen.getByText("tcp:443")).toBeVisible();
    await expect.element(screen.getByText("Showing 2 of 2 services")).toBeVisible();
  });

  it("narrows the cards to the search, and offers to clear one that matches nothing", async () => {
    const screen = await render(page());
    const search = screen.getByRole("searchbox");

    await search.fill("ops team");
    await expect.element(screen.getByText("Showing 1 of 2 services")).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "Company wiki" }))
      .not.toBeInTheDocument();

    await search.fill("nothing like it");
    await expect.element(screen.getByText("No services match")).toBeVisible();
    // The search box has its own clear button; this is the one in the empty state.
    await screen.getByRole("button", { name: "Clear search" }).last().click();
    await expect.element(screen.getByText("Showing 2 of 2 services")).toBeVisible();
  });

  it("says there are no services yet instead of an empty list", async () => {
    const screen = await render(page([]));

    await expect.element(screen.getByRole("heading", { name: "No services" })).toBeVisible();
    await expect.element(screen.getByRole("list", { name: "Services" })).not.toBeInTheDocument();
  });
});
