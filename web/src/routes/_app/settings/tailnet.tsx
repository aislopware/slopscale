import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { settingsQuery, tailnetLockQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ApprovalSection, DeviceTrustSection } from "~/components/settings/approval-section.tsx";
import { KeyExpirySection } from "~/components/settings/key-expiry-section.tsx";
import { SSHRecordingSection } from "~/components/settings/ssh-recording-section.tsx";
import { TailnetLockSection } from "~/components/settings/tailnet-lock-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";

export const Route = createFileRoute("/_app/settings/tailnet")({
  beforeLoad: requireScope("feature_settings:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(settingsQuery),
      context.queryClient.query(tailnetLockQuery),
    ]);
  },
  component: TailnetSettingsPage,
});

function TailnetSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(settingsQuery).data;
  const lock = useSuspenseQuery(tailnetLockQuery).data;
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Tailnet"
        description="Who may join, what a machine must prove, how long a login lasts and whether SSH sessions are recorded."
      />
      <div className="flex flex-col gap-6">
        <ApprovalSection settings={settings} canEdit={canEdit} />
        <DeviceTrustSection settings={settings} canEdit={canEdit} />
        <KeyExpirySection settings={settings} canEdit={canEdit} />
        <SSHRecordingSection settings={settings} canEdit={canEdit} />
        <TailnetLockSection lock={lock} canEdit={canEdit} />
      </div>
    </>
  );
}
