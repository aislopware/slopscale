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

import type { Me } from "~/auth/me.ts";
import { CreateFederatedIdentityPage } from "~/components/keys/federated-page.tsx";

const me: Me = {
  kind: "api_key",
  role: "owner",
  allAccess: false,
  scoped: true,
  scopes: ["oauth_keys"],
  permissions: { oauth_keys: true },
};

function app(): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <CreateFederatedIdentityPage me={me} />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/keys/oauth" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

describe(CreateFederatedIdentityPage, () => {
  it("suggests this server as the audience", async () => {
    const screen = await render(app());

    await expect.element(screen.getByLabelText("Audience")).toHaveValue(window.location.origin);
  });

  // Required fields stay quiet until the first save, then each one says what it needs.
  it("names every missing field on the first save and sends nothing", async () => {
    const screen = await render(app());

    expect(screen.getByText("Enter an issuer URL.").query()).toBeNull();
    await screen.getByRole("button", { name: "Create identity" }).click();

    await expect.element(screen.getByText("Enter an issuer URL.")).toBeVisible();
    await expect.element(screen.getByText("Enter the subject the token carries.")).toBeVisible();
    await expect.element(screen.getByText("Pick at least one scope.")).toBeVisible();
    expect(screen.getByText("The identity was not created").query()).toBeNull();
  });

  it("clears a field's error once it is filled", async () => {
    const screen = await render(app());

    await screen.getByRole("button", { name: "Create identity" }).click();
    await expect.element(screen.getByText("Enter an issuer URL.")).toBeVisible();
    await screen.getByLabelText("Issuer").fill("https://token.actions.githubusercontent.com");

    await expect.element(screen.getByText("Enter an issuer URL.")).not.toBeInTheDocument();
  });
});
