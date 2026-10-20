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

import type { Me } from "~/auth/me.ts";
import { AreaSubNav } from "~/components/layout/sub-nav.tsx";
import { pageGridClass } from "~/components/ui/page.tsx";

const allAccess: Me = {
  kind: "api_key",
  role: "member",
  allAccess: true,
  scoped: false,
  scopes: [],
  permissions: { all: true, "dns:read": true },
};

/** The DNS branch's pages and a page under one of them, in the shell's page grid. */
function app(path: string): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <div className={`${pageGridClass} [--app-bar-height:58px] [--app-header-height:58px]`}>
        <AreaSubNav me={allAccess} area="/dns" />
        <Outlet />
      </div>
    ),
  });
  const pages = ["/dns/nameservers", "/dns/split", "/dns/records", "/dns/split/new"].map((page) =>
    createRoute({
      getParentRoute: () => rootRoute,
      path: page,
      component: () => <p data-testid="page">{page}</p>,
    }),
  );
  const router = createRouter({
    routeTree: rootRoute.addChildren(pages),
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  return <RouterProvider router={router} />;
}

describe(AreaSubNav, () => {
  // Design spec §13.2: a 53px sticky bar under the top bar, a 36px segmented track whose tabs are
  // the branch's addresses, and the branch's documentation on the right.
  it("links the branch's pages as tabs in a 53px bar under the top bar", async () => {
    const screen = await render(app("/dns/split"));
    const bar = screen.getByRole("navigation", { name: "DNS" });

    await expect.element(bar).toBeVisible();
    expect(bar.element().getBoundingClientRect().height).toBe(53);
    expect(getComputedStyle(bar.element()).position).toBe("sticky");
    expect(getComputedStyle(bar.element()).top).toBe("58px");
    expect(screen.getByRole("tablist").element().getBoundingClientRect().height).toBe(36);
    await expect
      .element(screen.getByRole("tab", { name: "Split DNS" }))
      .toHaveAttribute("aria-selected", "true");
    await expect
      .element(screen.getByRole("tab", { name: "Extra records" }))
      .toHaveAttribute("href", "/dns/records");
    await expect
      .element(screen.getByRole("link", { name: "DNS documentation" }))
      .toHaveAttribute("href", expect.stringContaining("ref/dns"));
  });

  it("moves to another page from its tab", async () => {
    const screen = await render(app("/dns/split"));

    await screen.getByRole("tab", { name: "Nameservers" }).click();

    await expect.element(screen.getByTestId("page")).toHaveTextContent("/dns/nameservers");
    await expect
      .element(screen.getByRole("tab", { name: "Nameservers" }))
      .toHaveAttribute("aria-selected", "true");
  });

  // What sticks inside the page has to rest under the bar, not behind it.
  it("raises the page's sticky offset by its height", async () => {
    const screen = await render(app("/dns/records"));
    const page = screen.getByTestId("page").element();

    await expect.element(screen.getByRole("navigation", { name: "DNS" })).toBeVisible();
    expect(getComputedStyle(page).getPropertyValue("--app-header-height").trim()).toBe("111px");
  });

  it("leaves a page under one of the branch's pages to its own header", async () => {
    const screen = await render(app("/dns/split/new"));

    await expect.element(screen.getByTestId("page")).toBeVisible();
    await expect.element(screen.getByRole("navigation", { name: "DNS" })).not.toBeInTheDocument();
  });
});
