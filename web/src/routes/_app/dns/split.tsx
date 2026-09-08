import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { dnsQuery, dnsRulesQuery, groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { editableSettings } from "~/components/dns/model.ts";
import { useDnsMutations } from "~/components/dns/mutations.ts";
import { useDnsRuleMutations } from "~/components/dns/rule-mutations.ts";
import { DnsRulesSection } from "~/components/dns/rules-section.tsx";
import { SourceBanner } from "~/components/dns/source.tsx";
import { SplitDnsSection } from "~/components/dns/split-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/dns/split")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(dnsQuery),
      context.queryClient.query(dnsRulesQuery),
    ]);
  },
  component: SplitDnsPage,
});

function SplitDnsPage(): ReactElement {
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
        title="Split DNS"
        description="Domains answered by nameservers of their own, for every machine or only for the groups you pick."
      />
      <div className="flex flex-col gap-6">
        <SourceBanner dns={dns.data} canEdit={canEdit} mutations={mutations} />
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
      </div>
    </>
  );
}
