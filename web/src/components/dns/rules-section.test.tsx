import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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

function renderSection(rules: readonly DnsRule[], canEdit: boolean): ReturnType<typeof render> {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <Harness rules={rules} canEdit={canEdit} />
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
    await expect.element(screen.getByRole("button", { name: "Add rule" })).not.toBeInTheDocument();
  });

  it("opens the editor from a rule and the creator from the header", async () => {
    const screen = await renderSection([rule], true);

    await screen.getByRole("button", { name: "Edit DNS rule Corp DNS" }).click();
    await expect.element(screen.getByRole("dialog", { name: "Edit DNS rule" })).toBeVisible();
    await expect.element(screen.getByLabelText("Name")).toHaveValue("Corp DNS");
  });

  it("refuses to create a rule until it has a name, domains, nameservers and a group", async () => {
    const screen = await renderSection([], true);

    await screen.getByRole("button", { name: "Add rule" }).click();
    await expect.element(screen.getByRole("dialog", { name: "New DNS rule" })).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Create rule" })).toBeDisabled();
  });
});
