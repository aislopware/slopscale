import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { render } from "vitest-browser-react";

import type { Settings } from "~/api/queries.ts";
import { RouteSelectionSection } from "~/components/settings/route-selection-section.tsx";

const settings: Settings = {
  devicesApprovalOn: false,
  usersApprovalOn: false,
  postureIdentityOn: false,
  deviceAttributesOn: false,
  keyExpiryDays: 0,
  defaultKeyExpiryDays: 180,
  sshRecorders: [],
  sshRecordingEnforce: false,
  embeddedRecorder: false,
  routeSelection: "regional-routing",
};

function app(children: ReactNode): ReactElement {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe(RouteSelectionSection, () => {
  it("names the mode in force and what it does", async () => {
    const screen = await render(app(<RouteSelectionSection settings={settings} canEdit />));

    await expect.element(screen.getByText("Route selection")).toBeVisible();
    await expect
      .element(screen.getByRole("combobox", { name: "Route selection" }))
      .toHaveTextContent("Regional routing");
    await expect.element(screen.getByText(/nearest it by measured round trip/u)).toBeVisible();
  });

  it("describes active-passive failover as one router for everyone", async () => {
    const screen = await render(
      app(
        <RouteSelectionSection
          settings={{ ...settings, routeSelection: "active-passive-failover" }}
          canEdit
        />,
      ),
    );

    await expect
      .element(screen.getByRole("combobox", { name: "Route selection" }))
      .toHaveTextContent("Active-passive failover");
    await expect.element(screen.getByText(/Every machine uses the same router/u)).toBeVisible();
  });

  it("is read-only without the settings scope", async () => {
    const screen = await render(app(<RouteSelectionSection settings={settings} canEdit={false} />));

    await expect.element(screen.getByRole("combobox", { name: "Route selection" })).toBeDisabled();
  });
});
