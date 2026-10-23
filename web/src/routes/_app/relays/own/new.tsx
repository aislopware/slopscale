import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { RegionEditor } from "~/components/derp/region-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/relays/own/new")({
  // A reader who may not change relays has nothing to do here; the list's button is off for them.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "feature_settings")) {
      throw redirect({ to: "/relays/own", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: NewRegionPage,
});

function NewRegionPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;

  useBreadcrumb("New region");

  return (
    <FormLayout>
      <PageHeader
        title="New region"
        docs="ref/derp"
        description="A region of relays you run yourself, merged into the map on top of the sources."
      />
      <RegionEditor derp={derp} canEdit={can(me, "feature_settings")} />
    </FormLayout>
  );
}
