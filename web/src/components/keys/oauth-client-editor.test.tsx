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

import type { OAuthClient } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { OAuthClientEditor } from "~/components/keys/oauth-client-editor.tsx";

const me: Me = {
  kind: "api_key",
  role: "owner",
  allAccess: false,
  scoped: true,
  scopes: ["oauth_keys", "devices:core"],
  permissions: { oauth_keys: true, "devices:core": true },
};

const federated: OAuthClient = {
  clientId: "f1",
  keyType: "federated",
  description: "CI deploy",
  issuer: "https://token.actions.githubusercontent.com",
  audience: "https://scale.example.com",
  subject: "repo:acme/infrastructure:ref:refs/heads/main",
  customClaimRules: { workflow: "Deploy" },
  scopes: ["devices:core"],
  tags: ["tag:ci"],
  userId: null,
  createdAt: "2026-01-01T12:00:00Z",
};

function app(client: OAuthClient, canEdit = true): ReactElement {
  const rootRoute = createRootRoute({
    component: () => (
      <OAuthClientEditor
        client={client}
        me={me}
        canEdit={canEdit}
        users={[]}
        serverUrl="https://scale.example.com"
      />
    ),
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

describe(OAuthClientEditor, () => {
  // The list truncates the subject to fit its column; the page is where the whole record is.
  it("shows the client's facts and its trust conditions in full", async () => {
    const screen = await render(app(federated));

    await expect.element(screen.getByRole("button", { name: "Copy client id" })).toBeVisible();
    await expect.element(screen.getByText("Federated identity")).toBeVisible();
    await expect
      .element(screen.getByLabelText("Subject"))
      .toHaveValue("repo:acme/infrastructure:ref:refs/heads/main");
    await expect.element(screen.getByRole("button", { name: "Copy token exchange" })).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Delete identity" })).toBeVisible();
  });

  it("refuses to save an identity without a subject and says why", async () => {
    const screen = await render(app(federated));

    await screen.getByLabelText("Subject").fill("");
    await screen.getByRole("button", { name: "Save identity" }).click();

    await expect.element(screen.getByText("Enter the subject the token carries.")).toBeVisible();
    await expect.element(screen.getByLabelText("Subject")).toHaveAttribute("aria-invalid", "true");
  });

  it("says when the issuer is not https as soon as the field is left", async () => {
    const screen = await render(app(federated));

    await screen.getByLabelText("Issuer").fill("http://token.example.com");
    await screen.getByLabelText("Subject").click();

    await expect.element(screen.getByText("The issuer URL must use https://")).toBeVisible();
  });

  it("keeps the page and shows the server's refusal when the save fails", async () => {
    const screen = await render(app(federated));

    await screen.getByLabelText("Identity description").fill("CI deploy, prod");
    await screen.getByRole("button", { name: "Save identity" }).click();

    await expect.element(screen.getByText("The identity was not saved")).toBeVisible();
    await expect
      .element(screen.getByLabelText("Identity description"))
      .toHaveValue("CI deploy, prod");
  });

  it("lets a reader see the client but offers no delete", async () => {
    const screen = await render(
      app({ ...federated, keyType: "client", issuer: "", audience: "", subject: "" }, false),
    );

    await expect.element(screen.getByText("Read only")).toBeVisible();
    await expect.element(screen.getByText("OAuth client")).toBeVisible();
    expect(screen.getByRole("button", { name: "Delete client" }).query()).toBeNull();
    expect(screen.getByLabelText("Issuer").query()).toBeNull();
  });
});
