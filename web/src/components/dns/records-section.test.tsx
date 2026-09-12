import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Dns } from "~/api/queries.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { ExtraRecordsSection } from "~/components/dns/records-section.tsx";

const settings: Dns["effective"] = {
  extraRecords: [{ name: "grafana.example.ts.net", type: "A", value: "100.64.0.3" }],
  nameservers: [],
  overrideLocalDns: false,
  searchDomains: [],
  splitNameservers: {},
  splitUseWithExitNode: {},
  useWithExitNode: [],
};

const dns: Dns = {
  baseDomain: "example.ts.net",
  effective: settings,
  extraRecordsPath: "",
  fromFile: settings,
  magicDns: true,
  overridden: false,
};

function Harness(): ReactElement {
  const mutations = useDnsMutations();

  return <ExtraRecordsSection dns={dns} canEdit mutations={mutations} />;
}

function renderSection(): ReturnType<typeof render> {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <Harness />
    </QueryClientProvider>,
  );
}

describe(ExtraRecordsSection, () => {
  it("lists a record", async () => {
    const screen = await renderSection();

    await expect.element(screen.getByText("grafana.example.ts.net")).toBeVisible();
    await expect.element(screen.getByText("100.64.0.3")).toBeVisible();
  });

  // The form once reset the mutation in an effect that depended on the mutation object, which
  // every render rebuilds and every reset re-renders: opening the dialog crashed the console.
  it("opens the form to add a record", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Add record" }).click();

    await expect.element(screen.getByRole("dialog", { name: "Add record" })).toBeVisible();
    await expect.element(screen.getByLabelText("Name")).toBeVisible();
    await expect.element(screen.getByLabelText("Value")).toBeVisible();
  });
});
