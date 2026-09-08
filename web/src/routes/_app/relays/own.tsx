import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import { RelaysSection } from "~/components/derp/relays-section.tsx";
import { SourceBanner, StaleSettingsBanner } from "~/components/derp/source.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays/own")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: OwnRelaysPage,
});

function OwnRelaysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const mutations = useDerpMutations();
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Your relays"
        description="Regions you add by hand, for relays you run yourself. They are merged into the map on top of the sources."
      />
      <div className="flex flex-col gap-6">
        <StaleSettingsBanner mutations={mutations} />
        <SourceBanner derp={derp} canEdit={canEdit} mutations={mutations} />
        <RelaysSection derp={derp} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
