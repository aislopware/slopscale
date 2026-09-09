import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { TailnetLock } from "~/api/queries.ts";
import { TailnetLockSection } from "~/components/settings/tailnet-lock-section.tsx";

const stamp = "2026-09-07T12:00:00Z";

const onLock: TailnetLock = {
  enabled: true,
  head: "head12345678",
  keys: [
    {
      id: "key-1",
      public: "tlpub:testpublickey123",
      votes: 1,
    },
  ],
  supportDisablementAvailable: true,
  signedNodeIds: ["1", "2"],
  unsignedNodeIds: ["3"],
  enabledAt: stamp,
};

const offLock: TailnetLock = {
  enabled: false,
  head: "",
  keys: [],
  supportDisablementAvailable: false,
  signedNodeIds: [],
  unsignedNodeIds: [],
  disabledAt: stamp,
};

function app(children: ReactNode): ReactElement {
  const rootRoute = createRootRoute({ component: () => <div>{children}</div> });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/machines/$nodeId" }),
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

describe(TailnetLockSection, () => {
  it("renders enabled lock with head, keys, machines and switch off action", async () => {
    const screen = await render(app(<TailnetLockSection lock={onLock} canEdit />));

    await expect.element(screen.getByText("Tailnet lock")).toBeVisible();
    await expect.element(screen.getByText("On")).toBeVisible();
    await expect.element(screen.getByText("head12345678")).toBeVisible();
    await expect.element(screen.getByText("tlpub:testpublickey123")).toBeVisible();
    await expect.element(screen.getByText("votes 1")).toBeVisible();
    await expect.element(screen.getByText("2")).toBeVisible();
    await expect.element(screen.getByText(/1 waiting for a signature/u)).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Switch off" })).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Switch off" })).toBeEnabled();
  });

  it("opens confirmation dialog when Switch off is clicked", async () => {
    const screen = await render(app(<TailnetLockSection lock={onLock} canEdit />));

    await screen.getByRole("button", { name: "Switch off" }).click();

    await expect.element(screen.getByRole("alertdialog")).toBeVisible();
    await expect.element(screen.getByText("Switch off tailnet lock?")).toBeVisible();
    await expect
      .element(screen.getByText("Every machine drops its lock state and key signatures."))
      .toBeVisible();
  });

  it("renders disabled lock without head or switch off action", async () => {
    const screen = await render(app(<TailnetLockSection lock={offLock} canEdit />));

    await expect.element(screen.getByText("Off")).toBeVisible();
    await expect.element(screen.getByText("None")).toBeVisible();
    await expect.element(screen.getByText("head12345678")).not.toBeInTheDocument();
    await expect
      .element(screen.getByRole("button", { name: "Switch off" }))
      .not.toBeInTheDocument();
  });

  it("disables Switch off when support disablement is unavailable", async () => {
    const noSecretLock: TailnetLock = {
      ...onLock,
      supportDisablementAvailable: false,
    };

    const screen = await render(app(<TailnetLockSection lock={noSecretLock} canEdit />));

    await expect.element(screen.getByRole("button", { name: "Switch off" })).toBeDisabled();
  });

  it("disables Switch off when caller cannot edit", async () => {
    const screen = await render(app(<TailnetLockSection lock={onLock} canEdit={false} />));

    await expect.element(screen.getByRole("button", { name: "Switch off" })).toBeDisabled();
  });
});
