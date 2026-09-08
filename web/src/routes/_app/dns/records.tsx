import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { dnsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { ExtraRecordsSection } from "~/components/dns/records-section.tsx";
import { SourceBanner } from "~/components/dns/source.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/dns/records")({
  loader: async ({ context }) => {
    await context.queryClient.query(dnsQuery);
  },
  component: RecordsPage,
});

function RecordsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const dns = useSuspenseQuery(dnsQuery);
  const mutations = useDnsMutations();
  const canEdit = can(me, "dns");

  return (
    <>
      <PageHeader
        title="Extra records"
        description="Names the server answers itself, on top of the ones MagicDNS gives every machine."
      />
      <div className="flex flex-col gap-6">
        <SourceBanner dns={dns.data} canEdit={canEdit} mutations={mutations} />
        <ExtraRecordsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
