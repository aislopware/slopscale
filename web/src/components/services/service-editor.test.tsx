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

import type { Service } from "~/api/queries.ts";
import { ServiceEditor } from "~/components/services/service-editor.tsx";

const stamp = "2026-01-01T12:00:00Z";

const web: Service = {
  id: "1",
  name: "svc:web",
  displayName: "Company wiki",
  dnsName: "web.example.ts.net",
  addresses: ["100.64.0.10"],
  ports: ["tcp:443"],
  comment: "",
  hosts: [],
  createdAt: stamp,
  updatedAt: stamp,
};

function page(service: Service | null): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <ServiceEditor service={service ?? undefined} canEdit />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren(
      ["/", "/services", "/services/$label"].map((path) =>
        createRoute({ getParentRoute: () => rootRoute, path }),
      ),
    ),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(ServiceEditor, () => {
  it("asks for a name when a new service is saved without one, and sends nothing", async () => {
    const screen = await render(page(null));

    await screen.getByRole("button", { name: "Create service" }).click();

    await expect.element(screen.getByText("Give the service a name.")).toBeVisible();
    await expect
      .element(screen.getByRole("textbox", { name: "Name" }))
      .toHaveAttribute("aria-invalid", "true");
  });

  it("says what is wrong with the name and the ports once they are left", async () => {
    const screen = await render(page(null));
    const name = screen.getByRole("textbox", { name: "Name" });
    const ports = screen.getByRole("textbox", { name: /^Ports/u });

    await name.fill("Web");
    await ports.click();
    await expect.element(screen.getByText("The name must be lower case.")).toBeVisible();

    await ports.fill("443");
    await name.click();
    await expect
      .element(screen.getByText('"443" is not a protocol and port, such as tcp:443.'))
      .toBeVisible();

    // Live after the first time it was left.
    await ports.fill("tcp:443");
    await expect
      .element(screen.getByText('"443" is not a protocol and port, such as tcp:443.'))
      .not.toBeInTheDocument();
  });

  it("shows the command that hosts the new service under the name being typed", async () => {
    const screen = await render(page(null));

    await screen.getByRole("textbox", { name: "Name" }).fill("grafana");

    await expect.element(screen.getByText("tailscale serve --service=svc:grafana")).toBeVisible();
  });

  it("edits an existing service without its name, and offers Delete", async () => {
    const screen = await render(page(web));

    await expect.element(screen.getByRole("textbox", { name: "Name" })).not.toBeInTheDocument();
    await expect
      .element(screen.getByRole("textbox", { name: /^Display name/u }))
      .toHaveValue("Company wiki");
    await expect.element(screen.getByRole("textbox", { name: /^Ports/u })).toHaveValue("tcp:443");
    await expect.element(screen.getByRole("button", { name: "Save service" })).toBeEnabled();
    await expect.element(screen.getByRole("button", { name: "Delete service" })).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "Cancel" }))
      .toHaveAttribute("href", "/services/web");
  });
});
