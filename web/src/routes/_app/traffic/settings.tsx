import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { trafficReportersQuery, trafficSettingsQuery } from "~/api/traffic.ts";
import { can } from "~/auth/me.ts";
import { ResolverNotices } from "~/components/traffic/resolver-notices.tsx";
import { TrafficSettingsSections } from "~/components/traffic/settings-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/traffic/settings")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(trafficSettingsQuery),
      context.queryClient.query(trafficReportersQuery),
    ]);
  },
  component: TrafficSettingsPage,
});

function TrafficSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(trafficSettingsQuery).data;
  const reporters = useSuspenseQuery(trafficReportersQuery).data;

  return (
    <>
      <PageHeader
        title="Settings"
        description="What the gateways collect and how long the server keeps it."
      />
      <ResolverNotices reporters={reporters} />
      <TrafficSettingsSections
        settings={settings}
        reporters={reporters}
        canEdit={can(me, "logs:network")}
        canEditDns={can(me, "logs:network") && can(me, "dns")}
      />
    </>
  );
}
