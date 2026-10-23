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

import type { App } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { AppEditor } from "~/components/apps/app-editor.tsx";

const stamp = "2026-01-01T12:00:00Z";

// Only the write scope: the tag suggestions read nothing, so the form makes no request on its own.
const operator: Me = {
  kind: "session",
  role: "admin",
  allAccess: false,
  scoped: false,
  scopes: [],
  permissions: { policy_file: true },
};

const crm: App = {
  id: "1",
  name: "crm",
  description: "Sales CRM",
  domains: ["crm.example.com"],
  connectors: ["tag:connector"],
  routes: [],
  nodes: [
    {
      connector: true,
      learnedRoutes: 4,
      name: "office-gw",
      nodeId: "7",
      online: true,
      owner: {
        tags: ["tag:connector"],
        userId: "",
        userName: "",
        displayName: "",
        profilePicUrl: "",
      },
      pending: 2,
    },
  ],
  createdAt: stamp,
  updatedAt: stamp,
};

function page(app: App | null, canEdit = true): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <AppEditor app={app ?? undefined} me={operator} canEdit={canEdit} />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren(
      ["/", "/apps", "/routes", "/machines/$nodeId"].map((path) =>
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

describe(AppEditor, () => {
  it("says what is missing when a new app is saved empty, and sends nothing", async () => {
    const screen = await render(page(null));

    await screen.getByRole("button", { name: "Create app" }).click();

    await expect.element(screen.getByText("Give the app a name.")).toBeVisible();
    await expect.element(screen.getByText("Add at least one domain or route.")).toBeVisible();
    await expect
      .element(screen.getByRole("textbox", { name: "Name" }))
      .toHaveAttribute("aria-invalid", "true");
  });

  it("clears the missing name as soon as it is typed", async () => {
    const screen = await render(page(null));

    await screen.getByRole("button", { name: "Create app" }).click();
    await expect.element(screen.getByText("Give the app a name.")).toBeVisible();

    await screen.getByRole("textbox", { name: "Name" }).fill("crm");
    await expect.element(screen.getByText("Give the app a name.")).not.toBeInTheDocument();
  });

  it("says a domain row is wrong once it is left", async () => {
    const screen = await render(page(null));

    await screen.getByRole("button", { name: "Add domain" }).click();
    await screen.getByRole("textbox", { name: "Domains 1" }).fill("not a domain");
    await screen.getByRole("textbox", { name: "Name" }).click();

    await expect
      .element(screen.getByText("Enter a domain such as example.com or *.example.com."))
      .toBeVisible();
  });

  it("opens an existing app with its fields, the machines serving it and Delete", async () => {
    const screen = await render(page(crm));

    await expect.element(screen.getByRole("textbox", { name: "Name" })).toHaveValue("crm");
    await expect.element(screen.getByRole("button", { name: "Save app" })).toBeEnabled();
    await expect.element(screen.getByRole("button", { name: "Delete app" })).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "office-gw" }))
      .toHaveAttribute("href", "/machines/7");
    await expect
      .element(screen.getByRole("link", { name: "2 routes pending" }))
      .toHaveAttribute("href", "/routes?pending=true&q=office-gw");
  });

  it("only shows the app to a role that may not change it", async () => {
    const screen = await render(page(crm, false));

    await expect.element(screen.getByText("Read only")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Save app" })).toBeDisabled();
    await expect
      .element(screen.getByRole("button", { name: "Delete app" }))
      .not.toBeInTheDocument();
  });
});
