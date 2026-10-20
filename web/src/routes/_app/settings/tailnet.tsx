import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { serverInfoQuery, settingsQuery, tailnetLockQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { ApprovalSection, DeviceTrustSection } from "~/components/settings/approval-section.tsx";
import { KeyExpirySection } from "~/components/settings/key-expiry-section.tsx";
import { SSHRecordingSection } from "~/components/settings/ssh-recording-section.tsx";
import { TailnetIDSection } from "~/components/settings/tailnet-id-section.tsx";
import { TailnetLockSection } from "~/components/settings/tailnet-lock-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { SettingsLayout } from "~/components/ui/settings-layout.tsx";
import type { SettingsPart } from "~/components/ui/settings-layout.tsx";

export const Route = createFileRoute("/_app/settings/tailnet")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(settingsQuery),
      context.queryClient.query(tailnetLockQuery),
      context.queryClient.query(serverInfoQuery),
    ]);
  },
  component: TailnetSettingsPage,
});

function TailnetSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const settings = useSuspenseQuery(settingsQuery).data;
  const lock = useSuspenseQuery(tailnetLockQuery).data;
  const info = useSuspenseQuery(serverInfoQuery).data;
  const canEdit = can(me, "feature_settings");

  const parts: SettingsPart[] = [
    { id: "general", label: "General" },
    { id: "approval", label: "Approval" },
    { id: "device-trust", label: "Device trust" },
    { id: "key-expiry", label: "Key expiry" },
    { id: "ssh-recording", label: "SSH recording" },
    { id: "tailnet-lock", label: "Tailnet lock" },
    ...(lock.enabled ? [{ id: "danger-zone", label: "Danger zone", danger: true }] : []),
  ];

  return (
    <>
      <PageHeader
        title="Tailnet"
        docs="ref/console"
        description="The tailnet's ID, who may join, what a machine must prove, how long a login lasts and whether SSH sessions are recorded."
      />
      <SettingsLayout parts={parts}>
        <TailnetIDSection tailnetId={info.tailnetId} />
        <ApprovalSection settings={settings} canEdit={canEdit} />
        <DeviceTrustSection settings={settings} canEdit={canEdit} />
        <KeyExpirySection settings={settings} canEdit={canEdit} />
        <SSHRecordingSection settings={settings} canEdit={canEdit} />
        <TailnetLockSection lock={lock} canEdit={canEdit} />
      </SettingsLayout>
    </>
  );
}
