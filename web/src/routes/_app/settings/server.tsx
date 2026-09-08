import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { serverInfoQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MaintenanceSection } from "~/components/settings/maintenance-section.tsx";
import { ServerSection } from "~/components/settings/server-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/settings/server")({
  loader: async ({ context }) => {
    await context.queryClient.query(serverInfoQuery);
  },
  component: ServerSettingsPage,
});

function ServerSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const info = useSuspenseQuery(serverInfoQuery).data;

  return (
    <>
      <PageHeader
        title="Server"
        description="The build, addresses and config file values of the server this console talks to, and its maintenance tasks."
      />
      <div className="flex flex-col gap-6">
        <ServerSection info={info} />
        <MaintenanceSection canRun={can(me, "devices:core")} />
      </div>
    </>
  );
}
