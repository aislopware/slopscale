import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { serverInfoQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MaintenanceSection } from "~/components/settings/maintenance-section.tsx";
import { ServerSection, serverParts } from "~/components/settings/server-section.tsx";
import { SettingsLayout } from "~/components/settings/settings-page.tsx";
import type { SettingsPart } from "~/components/settings/settings-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/settings/server")({
  loader: async ({ context }) => {
    await context.queryClient.query(serverInfoQuery);
  },
  component: ServerSettingsPage,
});

const parts: readonly SettingsPart[] = [
  ...serverParts,
  { id: "maintenance", label: "Maintenance" },
];

function ServerSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const info = useSuspenseQuery(serverInfoQuery).data;

  return (
    <>
      <PageHeader
        title="Server"
        docs="ref/configuration"
        description="Build, addresses and config file values of this server, and its maintenance tasks."
      />
      <SettingsLayout parts={parts}>
        <ServerSection info={info} />
        <MaintenanceSection canRun={can(me, "devices:core")} />
      </SettingsLayout>
    </>
  );
}
