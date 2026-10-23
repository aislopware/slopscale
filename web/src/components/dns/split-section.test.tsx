import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { DnsSettings } from "~/api/schema.gen.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { SplitDnsSection } from "~/components/dns/split-section.tsx";

const settings: DnsSettings = {
  extraRecords: [],
  nameservers: [],
  overrideLocalDns: false,
  searchDomains: [],
  splitNameservers: { "corp.example": ["10.0.0.53", "10.0.0.54"], "lab.example": ["10.1.0.53"] },
  splitUseWithExitNode: {},
  useWithExitNode: [],
};

function Harness(): ReactElement {
  const mutations = useDnsMutations();

  return <SplitDnsSection settings={settings} canEdit mutations={mutations} />;
}

function renderSection(): ReturnType<typeof render> {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe(SplitDnsSection, () => {
  it("adds a domain in a dialog, with both fields on one line", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Add domain" }).click();

    const dialog = screen.getByRole("dialog", { name: "Add split DNS domain" });

    await expect.element(dialog).toBeVisible();

    const domain = dialog.getByLabelText("Domain").element().getBoundingClientRect();
    const servers = dialog.getByLabelText("Nameservers").element().getBoundingClientRect();

    expect(servers.top).toBe(domain.top);
  });

  it("edits a domain in place and refuses one that already has resolvers", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Edit corp.example" }).click();

    const form = screen.getByRole("form", { name: "Edit corp.example" });

    await expect.element(form.getByLabelText("Nameservers")).toHaveValue("10.0.0.53, 10.0.0.54");

    await form.getByLabelText("Domain").fill("lab.example");
    await form.getByRole("button", { name: "Save" }).click();

    await expect
      .element(form.getByText("This domain already has resolvers. Edit that entry instead."))
      .toBeVisible();

    await form.getByRole("button", { name: "Cancel" }).click();

    await expect.element(form).not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Edit corp.example" })).toBeVisible();
  });
});
