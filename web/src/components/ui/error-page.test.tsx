import { LinkProvider } from "@cloudflare/kumo/utils";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import { ApiError } from "~/api/error.ts";
import { RouteError, RouteNotFound } from "~/components/ui/error-page.tsx";
import { AppLink } from "~/lib/link.tsx";

/** A router whose routes throw what a real one would, with the pages under test as its defaults. */
function app(path: string): ReactElement {
  const rootRoute = createRootRoute({ component: () => <Outlet /> });
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => <p>overview</p>,
  });
  const forbiddenRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/forbidden",
    loader: () => {
      throw new ApiError(
        403,
        { type: "about:blank", detail: "Only an owner can do that.", instance: "/api/x" },
        "403",
      );
    },
  });
  const downRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/down",
    loader: () => {
      throw new ApiError(502, undefined, "502 Bad Gateway");
    },
  });
  const endedRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/machines/$id",
    loader: () => {
      throw new ApiError(401, undefined, "401");
    },
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, forbiddenRoute, downRoute, endedRoute]),
    history: createMemoryHistory({ initialEntries: [path] }),
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: RouteNotFound,
  });

  return (
    <QueryClientProvider client={new QueryClient()}>
      <LinkProvider component={AppLink}>
        <RouterProvider router={router} />
      </LinkProvider>
    </QueryClientProvider>
  );
}

describe(RouteError, () => {
  it("says what a refusal means and keeps the server's words in the details", async () => {
    const screen = await render(app("/forbidden"));

    await expect
      .element(screen.getByRole("heading", { name: "You do not have access" }))
      .toBeVisible();
    await expect.element(screen.getByText("Only an owner can do that.")).toBeVisible();
    await expect.element(screen.getByText("HTTP 403")).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Back to overview" })).toBeVisible();

    await screen.getByRole("button", { name: "Details" }).click();
    await expect.element(screen.getByText("Request /api/x")).toBeVisible();
  });

  it("sends an ended session to sign in and back to the same page", async () => {
    const screen = await render(app("/machines/7?tab=routes"));
    const link = screen.getByRole("link", { name: "Sign in" });

    await expect.element(link).toBeVisible();

    const href = new URL(String(link.element().getAttribute("href")), globalThis.location.origin);

    expect(href.pathname).toBe("/login");
    expect(href.searchParams.get("redirect")).toBe("/machines/7?tab=routes");
  });

  it("offers a retry when the server did not answer", async () => {
    const screen = await render(app("/down"));

    await expect
      .element(screen.getByRole("heading", { name: "The server did not answer" }))
      .toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });
});

describe(RouteNotFound, () => {
  it("names the missing page and the way back", async () => {
    const screen = await render(app("/nowhere"));

    await expect.element(screen.getByRole("heading", { name: "Page not found" })).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Go back" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Back to overview" })).toBeVisible();
  });
});
