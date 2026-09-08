import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { dnsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MagicDnsSection } from "~/components/dns/magic-section.tsx";
import { editableSettings } from "~/components/dns/model.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { NameserversSection } from "~/components/dns/nameservers-section.tsx";
import { SearchDomainsSection } from "~/components/dns/search-section.tsx";
import { SourceBanner } from "~/components/dns/source.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/dns/nameservers")({
  loader: async ({ context }) => {
    await context.queryClient.query(dnsQuery);
  },
  component: NameserversPage,
});

function NameserversPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const dns = useSuspenseQuery(dnsQuery);
  const mutations = useDnsMutations();
  const canEdit = can(me, "dns");

  return (
    <>
      <PageHeader
        title="Nameservers"
        description="The resolvers every machine uses, MagicDNS, and the search domains that complete short names. Changes reach the machines at once."
      />
      <div className="flex flex-col gap-6">
        <SourceBanner dns={dns.data} canEdit={canEdit} mutations={mutations} />
        <MagicDnsSection dns={dns.data} />
        <NameserversSection
          settings={editableSettings(dns.data)}
          canEdit={canEdit}
          mutations={mutations}
        />
        <SearchDomainsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
