import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { render } from "vitest-browser-react";

import type { Me } from "~/auth/me.ts";
import { documentTitle, Shell } from "~/components/layout/shell.tsx";

const allAccess: Me = {
  kind: "api_key",
  role: "member",
  allAccess: true,
  scoped: false,
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

/**
 * Runs the body with the media query Kumo's Sidebar reads answering "narrow", which is how the
 * sidebar becomes a drawer. The suite shares one browser window, so the query is answered here
 * rather than resized; the classes that keep the desktop rail's controls out of the drawer are
 * checked in the browser instead.
 */
async function withDrawer(body: () => Promise<void>): Promise<void> {
  const real = globalThis.matchMedia.bind(globalThis);

  // A real MediaQueryList with its answer pinned: everything else about it keeps working.
  vi.stubGlobal("matchMedia", (query: string): MediaQueryList => {
    const list = real(query);

    return query.includes("max-width")
      ? Object.defineProperty(list, "matches", { value: true, configurable: true })
      : list;
  });

  try {
    await body();
  } finally {
    vi.unstubAllGlobals();
  }
}

/** Clicks a control the window's real width keeps hidden with `display: none`. */
function clickPart(root: Element, selector: string): void {
  root.querySelector(selector)?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
}

describe(documentTitle, () => {
  // The tab reads page first, so a row of tabs tells apart by what is in front.
  it("puts the page before its section and the product last, each once", () => {
    expect(documentTitle("Machines", "backup-nas")).toBe("backup-nas - Machines - Slopscale");
    expect(documentTitle("Machines", null)).toBe("Machines - Slopscale");
    expect(documentTitle("Keys", "Keys")).toBe("Keys - Slopscale");
    expect(documentTitle(undefined, null)).toBe("Slopscale");
  });
});

describe(Shell, () => {
  it("titles the tab after the page it shows", async () => {
    await render(app(allAccess));

    await expect.poll(() => document.title).toBe("Overview - Slopscale");
  });

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
      app({ ...allAccess, allAccess: false, scoped: false, permissions: { "users:read": true } }),
    );

    await expect.element(screen.getByRole("link", { name: "Users" })).toBeVisible();
    await expect.element(screen.getByRole("link", { name: "Machines" })).not.toBeInTheDocument();
    await expect.element(screen.getByRole("link", { name: "Audit log" })).not.toBeInTheDocument();
  });

  it("opens the quick search palette on the pages the caller may see", async () => {
    const screen = await render(
      app({ ...allAccess, allAccess: false, scoped: false, permissions: { "users:read": true } }),
    );

    await screen.getByRole("button", { name: /Quick search/u }).click();

    const palette = screen.getByRole("dialog");
    await expect.element(palette).toBeVisible();
    await expect.element(palette.getByText("Users")).toBeVisible();
    await expect.element(palette.getByText("Machines")).not.toBeInTheDocument();

    await palette.getByRole("combobox").fill("over");

    await expect.element(palette.getByText("Overview")).toBeVisible();
    await expect.element(palette.getByText("Users")).not.toBeInTheDocument();
  });

  it("shows the audit log to a caller that may read it", async () => {
    const screen = await render(
      app({
        ...allAccess,
        allAccess: false,
        scoped: false,
        permissions: { "logs:configuration:read": true },
      }),
    );

    await expect.element(screen.getByRole("link", { name: "Audit log" })).toBeVisible();
  });

  it("lists every page the caller may see in the palette", async () => {
    const screen = await render(
      app({
        ...allAccess,
        permissions: {
          "devices:core:read": true,
          "devices:routes:read": true,
          "dns:read": true,
          "feature_settings:read": true,
          "logs:configuration:read": true,
          "policy_file:read": true,
          "users:read": true,
          "webhooks:read": true,
        },
      }),
    );

    await screen.getByRole("button", { name: /Quick search/u }).click();

    const palette = screen.getByRole("dialog");

    // The pages group used to stop at eight rows, which cut off the last group of the sidebar. A
    // branch such as Settings is listed as its pages, each with the branch as its hint.
    await Promise.all(
      ["Overview", "Audit log", "SSH sessions", "Tailnet", "API keys", "Log streams"].map(
        async (page) => {
          await expect.element(palette.getByText(page, { exact: true })).toBeVisible();
        },
      ),
    );
    await expect.element(palette.getByText("Keys", { exact: true }).first()).toBeVisible();
  });

  it("opens the drawer and closes it with its own button", async () => {
    await withDrawer(async () => {
      const screen = await render(app(allAccess));

      // A closed drawer is out of the accessible tree, so nothing in it can be reached by mistake.
      await expect.element(screen.getByRole("link", { name: "Users" })).not.toBeInTheDocument();

      clickPart(screen.container, '[data-sidebar="trigger"]');
      await expect.element(screen.getByRole("link", { name: "Users" })).toBeVisible();

      clickPart(screen.container, '[data-sidebar="close"]');
      await expect.element(screen.getByRole("link", { name: "Users" })).not.toBeInTheDocument();
    });
  });

  it("closes the drawer when the scrim is clicked", async () => {
    await withDrawer(async () => {
      const screen = await render(app(allAccess));

      clickPart(screen.container, '[data-sidebar="trigger"]');
      await expect.element(screen.getByRole("link", { name: "Users" })).toBeVisible();

      clickPart(screen.container, "[data-sidebar-backdrop]");
      await expect.element(screen.getByRole("link", { name: "Users" })).not.toBeInTheDocument();
    });
  });
});
