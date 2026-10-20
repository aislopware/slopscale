import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { DnsSettings } from "~/api/schema.gen.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { NameserversSection } from "~/components/dns/nameservers-section.tsx";

const base: DnsSettings = {
  nameservers: ["1.1.1.1"],
  overrideLocalDns: true,
  splitNameservers: {},
  useWithExitNode: [],
  splitUseWithExitNode: {},
  searchDomains: [],
  extraRecords: [],
};

function Harness({ settings }: { readonly settings: DnsSettings }): ReactElement {
  const mutations = useDnsMutations();

  return <NameserversSection settings={settings} canEdit mutations={mutations} />;
}

function renderSection(settings: DnsSettings): ReturnType<typeof render> {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return render(
    <QueryClientProvider client={client}>
      <Harness settings={settings} />
    </QueryClientProvider>,
  );
}

/** How far two right edges may sit apart and still count as one edge, in pixels. */
const edgeTolerance = 1;

describe(NameserversSection, () => {
  it("says what an empty list means as an empty state, not as a sentence in a row", async () => {
    const screen = await renderSection({ ...base, nameservers: [], overrideLocalDns: false });

    await expect
      .element(screen.getByRole("heading", { name: "No global nameservers" }))
      .toBeVisible();
  });

  it("ends a nameserver's toggle on the same edge as the override toggle", async () => {
    const screen = await renderSection(base);
    const row = screen.getByRole("switch", { name: "Use with exit node for 1.1.1.1" }).element();
    const override = screen.getByRole("switch", { name: "Override local DNS" }).element();

    expect(
      Math.abs(row.getBoundingClientRect().right - override.getBoundingClientRect().right),
    ).toBeLessThanOrEqual(edgeTolerance);
  });

  it("keeps the exit node toggle off limits while local DNS is not overridden", async () => {
    const screen = await renderSection({ ...base, overrideLocalDns: false });

    await expect
      .element(screen.getByRole("switch", { name: "Use with exit node for 1.1.1.1" }))
      .toBeDisabled();
  });

  it("stages nameservers in a panel and saves them as one change", async () => {
    const screen = await renderSection(base);

    await screen.getByRole("button", { name: "Add nameserver" }).click();
    await expect.element(screen.getByRole("button", { name: "Add nameserver" })).toBeDisabled();
    await expect.element(screen.getByRole("button", { name: "Save 0 entries" })).toBeDisabled();

    await screen.getByLabelText("Nameserver").fill("9.9.9.9");
    await screen.getByRole("button", { name: "Add to list" }).click();
    await screen.getByLabelText("Nameserver").fill("8.8.8.8");
    await screen.getByRole("button", { name: "Add to list" }).click();

    await expect.element(screen.getByText("Unsaved changes")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Save 2 entries" })).toBeEnabled();

    await screen.getByRole("button", { name: "Remove 9.9.9.9 from the list" }).click();
    await expect.element(screen.getByRole("button", { name: "Save 1 entry" })).toBeEnabled();
  });

  it("refuses a nameserver that is malformed or already listed, inline", async () => {
    const screen = await renderSection(base);

    await screen.getByRole("button", { name: "Add nameserver" }).click();
    await screen.getByLabelText("Nameserver").fill("1.1.1.1");
    await screen.getByRole("button", { name: "Add to list" }).click();

    await expect.element(screen.getByText("That one is already on the list.")).toBeVisible();
    await expect.element(screen.getByRole("button", { name: "Save 0 entries" })).toBeDisabled();

    await screen.getByLabelText("Nameserver").fill("not a resolver");
    await screen.getByRole("button", { name: "Add to list" }).click();

    await expect.element(screen.getByText("Unsaved changes")).not.toBeInTheDocument();
  });

  it("closes the panel on cancel and gives the add button back", async () => {
    const screen = await renderSection(base);

    await screen.getByRole("button", { name: "Add nameserver" }).click();
    await screen.getByRole("button", { name: "Cancel" }).click();

    await expect
      .element(screen.getByRole("heading", { name: "Add nameservers" }))
      .not.toBeInTheDocument();
    await expect.element(screen.getByRole("button", { name: "Add nameserver" })).toBeEnabled();
  });
});
