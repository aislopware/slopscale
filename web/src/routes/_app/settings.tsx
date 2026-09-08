import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { serverInfoQuery, settingsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ApprovalSection, DeviceTrustSection } from "~/components/settings/approval-section.tsx";
import { KeyExpirySection } from "~/components/settings/key-expiry-section.tsx";
import { MaintenanceSection } from "~/components/settings/maintenance-section.tsx";
import { ServerSection } from "~/components/settings/server-section.tsx";
import { SessionSection } from "~/components/settings/session-section.tsx";
import { SSHRecordingSection } from "~/components/settings/ssh-recording-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/settings")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(settingsQuery),
      context.queryClient.query(serverInfoQuery),
    ]);
  },
  component: SettingsPage,
});

function SettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(settingsQuery).data;
  const info = useSuspenseQuery(serverInfoQuery).data;
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Settings"
        description="Tailnet-wide switches, the session this browser holds and the server it talks to."
      />
      <div className="flex flex-col gap-6">
        <ApprovalSection settings={settings} canEdit={canEdit} />
        <DeviceTrustSection settings={settings} canEdit={canEdit} />
        <KeyExpirySection settings={settings} canEdit={canEdit} />
        <SSHRecordingSection settings={settings} canEdit={canEdit} />
        <MaintenanceSection canRun={can(me, "devices:core")} />
        <SessionSection me={me} />
        <ServerSection info={info} />
      </div>
    </>
  );
}
