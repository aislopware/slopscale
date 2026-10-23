import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { EmbeddedSection } from "~/components/derp/embedded-section.tsx";
import { useDerpMutations } from "~/components/derp/mutations.ts";
import {
  DerpSourceActions,
  DerpSourceNote,
  StaleSettingsBanner,
} from "~/components/derp/source.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/relays/embedded")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: EmbeddedPage,
});

function EmbeddedPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const mutations = useDerpMutations();
  const canEdit = can(me, "feature_settings");

  return (
    <>
      <PageHeader
        title="Embedded relay"
        docs="ref/derp"
        description="The relay built into this server, the region it joins and how it verifies clients."
        meta={<DerpSourceNote derp={derp} />}
        actions={<DerpSourceActions derp={derp} canEdit={canEdit} mutations={mutations} />}
      />
      <StaleSettingsBanner mutations={mutations} />
      <EmbeddedSection derp={derp} canEdit={canEdit} mutations={mutations} />
    </>
  );
}
