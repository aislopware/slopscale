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
import { useDnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { DnsRulesSection } from "~/components/dns/rules-section.tsx";

const stamp = "2026-09-07T00:00:00Z";

const groups: Group[] = [
  {
    id: "2",
    name: "Engineering",
    description: "",
    builtin: "",
    source: "",
    requestable: false,
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
  description: "",
  enabled: false,
  domains: ["corp.example"],
  nameservers: ["10.0.0.53"],
  groupIds: ["2"],
  createdAt: stamp,
  updatedAt: stamp,
};

function Harness({
  rules,
  canEdit,
}: {
  readonly rules: readonly DnsRule[];
  readonly canEdit: boolean;
}): ReactElement {
  const mutations = useDnsRuleMutations();

  return <DnsRulesSection rules={rules} groups={groups} canEdit={canEdit} mutations={mutations} />;
}

/** The table cell a control sits in. */
function cellOf(element: Element): Element {
  const cell = element.closest("td");

  if (cell === null) {
    throw new Error("not in a cell");
  }

  return cell;
}

function renderSection(rules: readonly DnsRule[], canEdit: boolean): ReturnType<typeof render> {
  const rootRoute = createRootRoute({
    component: () => <Harness rules={rules} canEdit={canEdit} />,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      createRoute({ getParentRoute: () => rootRoute, path: "/" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/dns/split/rules/new" }),
      createRoute({ getParentRoute: () => rootRoute, path: "/dns/split/rules/$ruleId" }),
    ]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

describe(DnsRulesSection, () => {
  it("lists a rule with its domains, nameservers, group and state", async () => {
    const screen = await renderSection([rule], false);

    await expect.element(screen.getByText("Corp DNS")).toBeVisible();
    await expect.element(screen.getByText("corp.example")).toBeVisible();
    await expect.element(screen.getByText("10.0.0.53")).toBeVisible();
    await expect.element(screen.getByText("Engineering")).toBeVisible();
    await expect.element(screen.getByText("Disabled")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Add rule" })).toBeDisabled();

    await screen.getByRole("button", { name: "Actions for DNS rule Corp DNS" }).click();
    await expect.element(screen.getByRole("menuitem", { name: "View" })).toBeVisible();
    await expect.element(screen.getByRole("menuitem", { name: "Delete…" })).toBeDisabled();
  });

  it("links a rule to its page and the header to the creator", async () => {
    const screen = await renderSection([rule], true);

    await expect
      .element(screen.getByRole("link", { name: "Corp DNS" }))
      .toHaveAttribute("href", "/dns/split/rules/7");
    await expect
      .element(screen.getByRole("link", { name: "Add rule" }))
      .toHaveAttribute("href", "/dns/split/rules/new");
  });

  it("asks before deleting a rule", async () => {
    const screen = await renderSection([rule], true);

    await screen.getByRole("button", { name: "Actions for DNS rule Corp DNS" }).click();
    await screen.getByRole("menuitem", { name: "Delete…" }).click();

    await expect
      .element(screen.getByRole("alertdialog", { name: "Delete DNS rule?" }))
      .toBeVisible();
  });

  // Design spec §4.A: the ⋯ column is pinned like every other table's, and the nameservers wrap
  // between addresses, never inside one.
  it("pins the menu to the right edge and keeps each nameserver whole", async () => {
    const screen = await renderSection(
      [{ ...rule, nameservers: ["2001:4860:4860::8888", "149.112.112.112"] }],
      true,
    );
    const button = screen.getByRole("button", { name: "Actions for DNS rule Corp DNS" });

    await expect.element(button).toBeVisible();

    const cell = cellOf(button.element());
    const address = screen.getByText("2001:4860:4860::8888", { exact: false }).element();

    expect(getComputedStyle(cell).position).toBe("sticky");
    expect(getComputedStyle(address).whiteSpace).toBe("nowrap");
  });
});
