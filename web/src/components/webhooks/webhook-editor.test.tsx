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

import type { Webhook } from "~/api/queries.ts";
import { WebhookEditor } from "~/components/webhooks/webhook-editor.tsx";

const webhook: Webhook = {
  createdAt: "2026-01-01T12:00:00Z",
  createdByUserId: "1",
  updatedAt: "2026-01-01T12:00:00Z",
  description: "Ops channel",
  id: "7",
  lastDeliveryAt: null,
  lastDeliveryStatus: "",
  providerType: "slack",
  subscriptions: ["nodeCreated"],
  url: "https://hooks.slack.com/services/T0/B0/x",
};

function app(editing?: Webhook): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <WebhookEditor webhook={editing} eventTypes={["nodeCreated", "nodeDeleted"]} canEdit />
    ),
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/integrations/webhooks" }),
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

describe(WebhookEditor, () => {
  it("says what is missing only once a save is tried", async () => {
    const screen = await render(app());
    const url = screen.getByRole("textbox", { name: "URL" });

    await expect.element(url).not.toHaveAttribute("aria-invalid");
    await screen.getByRole("button", { name: "Create webhook" }).click();

    await expect.element(url).toHaveAttribute("aria-invalid", "true");
    await expect.element(screen.getByText("Enter the URL.")).toBeVisible();
    await expect.element(screen.getByText("Pick at least one event.")).toBeVisible();
    await expect.element(url).toHaveFocus();
  });

  it("flags a malformed address when the field is left, before any save", async () => {
    const screen = await render(app());
    const url = screen.getByRole("textbox", { name: "URL" });

    await url.fill("not a url");
    await screen.getByRole("textbox", { name: /Description/v }).click();

    await expect
      .element(screen.getByText("Enter a full URL, like https://example.com/hook"))
      .toBeVisible();
  });

  it("puts the server's refusal in a banner over the form", async () => {
    // The test server has no API, so the save is refused as it would be by a server that says no.
    const screen = await render(app(webhook));

    await screen.getByRole("button", { name: "Save webhook" }).click();

    await expect.element(screen.getByText("The webhook was not saved")).toBeVisible();
  });
});
