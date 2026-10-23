import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";
import { userEvent } from "vitest/browser";

import type { Dns } from "~/api/queries.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { ExtraRecordsSection } from "~/components/dns/records-section.tsx";

const settings: Dns["effective"] = {
  extraRecords: [
    { name: "grafana.example.ts.net", type: "A", value: "100.64.0.3" },
    { name: "wiki.example.ts.net", type: "A", value: "100.64.0.4" },
  ],
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

  it("edits a record in place under its row, one row at a time", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Edit record grafana.example.ts.net" }).click();

    const form = screen.getByRole("form", { name: "Edit grafana.example.ts.net" });

    await expect.element(form).toBeVisible();
    await expect.element(screen.getByLabelText("Name")).toHaveValue("grafana.example.ts.net");
    await expect.element(screen.getByLabelText("Name")).toHaveFocus();
    // Save waits for a change, and the row's Edit turned into Close.
    await expect.element(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    await expect
      .element(screen.getByRole("button", { name: "Close record grafana.example.ts.net" }))
      .toBeVisible();

    await screen.getByRole("button", { name: "Edit record wiki.example.ts.net" }).click();

    await expect.element(form).not.toBeInTheDocument();
    await expect.element(screen.getByLabelText("Name")).toHaveValue("wiki.example.ts.net");
  });

  it("checks the value once it is left and cancels on Escape", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Edit record grafana.example.ts.net" }).click();
    await screen.getByLabelText("Value").fill("not an address");
    await expect.element(screen.getByRole("button", { name: "Save" })).toBeEnabled();
    await screen.getByLabelText("Name").click();

    await expect.element(screen.getByText("An A record needs an IPv4 address.")).toBeVisible();

    await userEvent.keyboard("{Escape}");

    await expect
      .element(screen.getByRole("form", { name: "Edit grafana.example.ts.net" }))
      .not.toBeInTheDocument();
    await expect
      .element(screen.getByRole("button", { name: "Edit record grafana.example.ts.net" }))
      .toHaveFocus();
  });

  it("shows the server's refusal in the row's editor", async () => {
    const screen = await renderSection();

    await screen.getByRole("button", { name: "Edit record grafana.example.ts.net" }).click();
    await screen.getByLabelText("Value").fill("100.64.0.9");
    // The test server has no API, so the save is refused for real.
    await screen.getByRole("button", { name: "Save" }).click();

    await expect.element(screen.getByText("Not saved")).toBeVisible();
    await expect
      .element(screen.getByRole("form", { name: "Edit grafana.example.ts.net" }))
      .toBeVisible();
  });
});
