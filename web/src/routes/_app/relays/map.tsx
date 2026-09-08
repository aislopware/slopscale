import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MapSection } from "~/components/derp/map-section.tsx";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import { FetchErrorBanner, SourceBanner, StaleSettingsBanner } from "~/components/derp/source.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays/map")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: MapPage,
});

function MapPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const mutations = useDerpMutations();
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Map"
        description="The DERP map every machine receives: the regions and relays that carry traffic between machines that cannot connect directly."
      />
      <div className="flex flex-col gap-6">
        <StaleSettingsBanner mutations={mutations} />
        <SourceBanner derp={derp} canEdit={canEdit} mutations={mutations} />
        <FetchErrorBanner derp={derp} />
        <MapSection derp={derp} />
      </div>
    </>
  );
}
