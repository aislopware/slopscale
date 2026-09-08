import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import { FetchErrorBanner, SourceBanner, StaleSettingsBanner } from "~/components/derp/source.tsx";
import { SourcesSection } from "~/components/derp/sources-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays/sources")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: SourcesPage,
});

function SourcesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const mutations = useDerpMutations();
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Sources"
        description="The map URLs and files the map is built from, and how often they are fetched again."
      />
      <div className="flex flex-col gap-6">
        <StaleSettingsBanner mutations={mutations} />
        <SourceBanner derp={derp} canEdit={canEdit} mutations={mutations} />
        <FetchErrorBanner derp={derp} />
        <SourcesSection derp={derp} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
