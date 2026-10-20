import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, posturesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PostureEditor } from "~/components/access/posture-editor.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/postures/new")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(posturesQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: NewPosturePage,
});

function NewPosturePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { geoIpAvailable } = useSuspenseQuery(posturesQuery).data;
  const { rules } = useSuspenseQuery(accessRulesQuery).data;

  useBreadcrumb("New posture");

  return (
    <>
      <PageHeader
        title="New posture"
        docs="ref/device-trust"
        description="Conditions a machine must meet before a rule that requires the posture applies to it."
      />
      <PostureEditor
        rules={rules}
        geoIpAvailable={geoIpAvailable}
        canEdit={can(me, "policy_file")}
      />
    </>
  );
}
