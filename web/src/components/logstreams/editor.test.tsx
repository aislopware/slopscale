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

import type { LogStream } from "~/api/queries.ts";
import { LogStreamEditor } from "~/components/logstreams/editor.tsx";

const stream: LogStream = {
  createdAt: "2026-01-01T12:00:00Z",
  createdByUserId: "1",
  updatedAt: "2026-01-01T12:00:00Z",
  delivered: 1204,
  destination: "splunk",
  dropped: 0,
  enabled: true,
  hasToken: true,
  id: "3",
  lastDeliveryAt: null,
  lastDeliveryStatus: "",
  name: "SIEM",
  url: "https://splunk.example.com:8088/services/collector/event",
};

function app(editing?: LogStream): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <LogStreamEditor stream={editing} canEdit />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/integrations/log-streams" }),
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

describe(LogStreamEditor, () => {
  it("asks for the name and URL on a save, and moves to the first", async () => {
    const screen = await render(app());

    await screen.getByRole("button", { name: "Create log stream" }).click();

    await expect.element(screen.getByText("Give the stream a name.")).toBeVisible();
    await expect.element(screen.getByText("Enter the URL to ship to.")).toBeVisible();
    await expect.element(screen.getByRole("textbox", { name: "Name" })).toHaveFocus();
  });

  it("keeps a stored token when the field is left blank", async () => {
    const screen = await render(app(stream));

    await expect
      .element(screen.getByText("Leave blank to keep the stored credential."))
      .toBeVisible();
    await screen.getByRole("button", { name: "Save log stream" }).click();

    // Nothing on the form is wrong, so the save goes out and the test server refuses it.
    await expect.element(screen.getByText("The log stream was not saved")).toBeVisible();
    expect(document.querySelector("[aria-invalid='true']")).toBeNull();
  });
});
