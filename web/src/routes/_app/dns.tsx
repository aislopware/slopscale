import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { dnsQuery, dnsRulesQuery, groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { MagicDnsSection } from "~/components/dns/magic-section.tsx";
import { editableSettings } from "~/components/dns/model.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { NameserversSection } from "~/components/dns/nameservers-section.tsx";
import { ExtraRecordsSection } from "~/components/dns/records-section.tsx";
import { useDnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { DnsRulesSection } from "~/components/dns/rules-section.tsx";
import { SearchDomainsSection } from "~/components/dns/search-section.tsx";
import { SourceBanner } from "~/components/dns/source.tsx";
import { SplitDnsSection } from "~/components/dns/split-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/dns")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(dnsQuery),
      context.queryClient.query(dnsRulesQuery),
    ]);
  },
  component: DnsPage,
});

function DnsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const dns = useSuspenseQuery(dnsQuery);
  const rules = useSuspenseQuery(dnsRulesQuery);
  // The group list needs its own scope; without it the rules still show their group ids.
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const mutations = useDnsMutations();
  const ruleMutations = useDnsRuleMutations();
  const canEdit = can(me, "dns");

  return (
    <>
      <PageHeader
        title="DNS"
        description="Nameservers, split DNS, search domains and extra records every machine receives, plus split DNS only some groups get. Changes reach the machines at once."
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
        <DnsRulesSection
          rules={rules.data.rules}
          groups={groups.data?.groups ?? []}
          canEdit={canEdit}
          mutations={ruleMutations}
        />
        <SearchDomainsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
        <ExtraRecordsSection dns={dns.data} canEdit={canEdit} mutations={mutations} />
      </div>
    </>
  );
}
