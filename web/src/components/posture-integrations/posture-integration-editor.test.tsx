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

import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { PostureIntegrationEditor } from "~/components/posture-integrations/posture-integration-editor.tsx";

const falcon: PostureProvider = {
  provider: "falcon",
  label: "CrowdStrike Falcon",
  prefix: "falcon:",
  fields: ["baseUrl", "clientId", "clientSecret"],
  baseUrlDefault: "https://api.crowdstrike.com",
  attributes: [{ name: "falcon:ztaScore", type: "number", description: "ZTA score" }],
  help: "https://falcon.crowdstrike.com",
};

const integration: PostureIntegration = {
  config: { baseUrl: "https://api.crowdstrike.com", clientId: "cid" },
  createdAt: "2026-01-01T12:00:00Z",
  enabled: true,
  hasSecret: true,
  id: "4",
  lastError: "",
  lastMatched: 12,
  lastSyncAt: "",
  name: "Falcon",
  prefix: "falcon:",
  provider: "falcon",
  updatedAt: "2026-01-01T12:00:00Z",
};

function app(editing?: PostureIntegration): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <PostureIntegrationEditor integration={editing} provider={falcon} canEdit />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/integrations/posture" }),
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

describe(PostureIntegrationEditor, () => {
  it("starts from the provider's defaults and lists what it writes", async () => {
    const screen = await render(app());

    await expect
      .element(screen.getByRole("textbox", { name: "Base URL" }))
      .toHaveValue("https://api.crowdstrike.com");
    await expect.element(screen.getByText("falcon:ztaScore")).toBeVisible();
    await expect
      .element(screen.getByRole("link", { name: "Setup guide" }))
      .toHaveAttribute("href", "https://falcon.crowdstrike.com");
  });

  it("tests nothing while the credentials are incomplete, and says which are missing", async () => {
    const screen = await render(app());

    await screen.getByRole("button", { name: "Test connection" }).click();

    await expect.element(screen.getByText("Enter the client ID.")).toBeVisible();
    await expect.element(screen.getByText("Enter the client secret.")).toBeVisible();
    await expect.element(screen.getByText("Give the integration a name.")).toBeVisible();
    await expect.element(screen.getByText("Connection verified")).not.toBeInTheDocument();
  });

  it("lets an edit keep the stored secret and shows a refused save in a banner", async () => {
    const screen = await render(app(integration));

    await expect.element(screen.getByText("Leave blank to keep the stored secret.")).toBeVisible();
    await screen.getByRole("button", { name: "Save integration" }).click();

    await expect.element(screen.getByText("The integration was not saved")).toBeVisible();
  });
});
