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

import type { Me } from "~/auth/me.ts";
import { Shell } from "~/components/layout/shell.tsx";

const allAccess: Me = {
  kind: "api_key",
  role: "member",
  allAccess: true,
  scopes: [],
  permissions: { all: true, "devices:core:read": true, "users:read": true },
};

function app(me: Me): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <Shell me={me}>page body</Shell>,
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/" });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(Shell, () => {
  it("opens the account menu for a key without a user", async () => {
    const screen = await render(app(allAccess));

    await expect.element(screen.getByText("page body")).toBeVisible();
    await screen.getByRole("button", { name: "Account" }).click();

    await expect.element(screen.getByRole("menu")).toBeVisible();
    await expect.element(screen.getByText("All-access key")).toBeVisible();
    await expect.element(screen.getByText("API key")).toBeVisible();
    await expect.element(screen.getByRole("menuitem", { name: "Sign out" })).toBeVisible();
  });

  it("hides pages the caller may not read", async () => {
    const screen = await render(
      app({ ...allAccess, allAccess: false, permissions: { "users:read": true } }),
    );

    await expect.element(screen.getByRole("link", { name: "Users" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Machines" })).not.toBeInTheDocument();
    await expect.element(screen.getByRole("link", { name: "Audit log" })).not.toBeInTheDocument();
  });

  it("shows the audit log to a caller that may read it", async () => {
    const screen = await render(
      app({
        ...allAccess,
        allAccess: false,
        permissions: { "logs:configuration:read": true },
      }),
    );

    await expect.element(screen.getByRole("link", { name: "Audit log" })).toBeVisible();
  });
});
