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

import type { DnsRule, Group } from "~/api/queries.ts";
import { DnsRuleEditor } from "~/components/dns/rule-editor.tsx";

const stamp = "2026-09-07T00:00:00Z";

const groups: Group[] = [
  {
    id: "2",
    name: "Engineering",
    description: "",
    builtin: "",
    source: "",
    requestable: false,
    autoApprove: false,
    maxRequestSeconds: 0,
    expiries: [],
    nodeIds: [],
    userIds: ["1"],
    createdAt: stamp,
    updatedAt: stamp,
  },
];

const rule: DnsRule = {
  id: "7",
  name: "Corp DNS",
  description: "Internal zones",
  enabled: true,
  domains: ["corp.example"],
  nameservers: ["10.0.0.53"],
  groupIds: ["2"],
  createdAt: stamp,
  updatedAt: stamp,
};

function app(editing?: DnsRule): ReactElement {
  const rootRoute = createRootRoute({
    component: () => <DnsRuleEditor rule={editing} groups={groups} canEdit />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/dns/split" }),
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

describe(DnsRuleEditor, () => {
  it("says what is missing when an empty rule is saved", async () => {
    const screen = await render(app());

    await screen.getByRole("button", { name: "Create DNS rule" }).click();

    await expect.element(screen.getByText("Give the rule a name.")).toBeVisible();
    await expect.element(screen.getByText("Enter at least one domain.")).toBeVisible();
    await expect.element(screen.getByText("Enter at least one nameserver.")).toBeVisible();
    await expect.element(screen.getByText("Pick the groups that get the rule.")).toBeVisible();
    await expect.element(screen.getByLabelText("Name")).toHaveFocus();
  });

  it("checks the domains once the field is left, not while it is empty", async () => {
    const screen = await render(app());
    const domains = screen.getByLabelText("Domains");

    await domains.click();
    await screen.getByLabelText("Nameservers").click();
    await expect.element(screen.getByText("Enter at least one domain.")).not.toBeInTheDocument();

    await domains.fill("not a domain");
    await screen.getByLabelText("Nameservers").click();

    await expect.element(screen.getByText("Use a domain such as corp.example.com.")).toBeVisible();
  });

  it("opens an existing rule with its description and shows the server's refusal", async () => {
    const screen = await render(app(rule));

    await expect.element(screen.getByLabelText("Name")).toHaveValue("Corp DNS");
    await expect
      .element(screen.getByLabelText("Description", { exact: false }))
      .toHaveValue("Internal zones");

    // The test server has no API, so the save is refused for real.
    await screen.getByRole("button", { name: "Save DNS rule" }).click();

    await expect.element(screen.getByText("The DNS rule was not saved")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Delete DNS rule" })).toBeVisible();
  });
});
