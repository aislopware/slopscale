import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { dnsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MagicDnsSection } from "~/components/dns/magic-section.tsx";
import { editableSettings } from "~/components/dns/model.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { NameserversSection } from "~/components/dns/nameservers-section.tsx";
import { ExtraRecordsSection } from "~/components/dns/records-section.tsx";
import { SearchDomainsSection } from "~/components/dns/search-section.tsx";
import { SourceBanner } from "~/components/dns/source.tsx";
import { SplitDnsSection } from "~/components/dns/split-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/dns")({
  loader: async ({ context }) => {
    await context.queryClient.query(dnsQuery);
  },
  component: DnsPage,
});

function DnsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const dns = useSuspenseQuery(dnsQuery);
  const mutations = useDnsMutations();
  const canEdit = can(me, "dns");

  return (
    <>
      <PageHeader
        title="DNS"
        description="Nameservers, split DNS, search domains and extra records every machine receives. Changes reach the machines at once."
      />
      <div className="flex max-w-3xl flex-col gap-6">
        <SourceBanner dns={dns.data} canEdit={canEdit} mutations={mutations} />
        <MagicDnsSection dns={dns.data} />
        <NameserversSection
          settings={editableSettings(dns.data)}
          canEdit={canEdit}
          mutations={mutations}
        />
        <SplitDnsSection
          settings={editableSettings(dns.data)}
          canEdit={canEdit}
          mutations={mutations}
        />
        <SearchDomainsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
        <ExtraRecordsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
