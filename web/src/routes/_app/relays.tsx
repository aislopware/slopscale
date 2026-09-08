import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { EmbeddedSection } from "~/components/derp/embedded-section.tsx";
import { MapSection } from "~/components/derp/map-section.tsx";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import { RelaysSection } from "~/components/derp/relays-section.tsx";
import { FetchErrorBanner, SourceBanner, StaleSettingsBanner } from "~/components/derp/source.tsx";
import { SourcesSection } from "~/components/derp/sources-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: RelaysPage,
});

function RelaysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const mutations = useDerpMutations();
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Relays"
        description="DERP relays carry traffic between machines that cannot connect directly and help them find each other. Changes fetch the maps and reach the machines at once."
      />
      <div className="flex flex-col gap-6">
        <StaleSettingsBanner mutations={mutations} />
        <SourceBanner derp={derp} canEdit={canEdit} mutations={mutations} />
        <FetchErrorBanner derp={derp} />
        <EmbeddedSection derp={derp} canEdit={canEdit} mutations={mutations} />
        <SourcesSection derp={derp} canEdit={canEdit} mutations={mutations} />
        <RelaysSection derp={derp} canEdit={canEdit} mutations={mutations} />
        <MapSection derp={derp} />
      </div>
    </>
  );
}
