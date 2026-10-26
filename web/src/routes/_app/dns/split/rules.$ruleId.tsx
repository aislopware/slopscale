import { LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { dnsRulesQuery, groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { DnsRuleEditor } from "~/components/dns/rule-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/dns/split/rules/$ruleId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(dnsRulesQuery),
      can(context.me, "policy_file:read") ? context.queryClient.query(groupsQuery) : null,
    ]);
  },
  component: DnsRulePage,
});

function DnsRulePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { ruleId } = Route.useParams();
  const { rules } = useSuspenseQuery(dnsRulesQuery).data;
  // The group list needs its own scope; without it the picker has nothing to offer.
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const found = rules.find((candidate) => candidate.id === ruleId);
  // The rule as the page last found it. A delete takes it out of the list before the editor has
  // gone back to the Split DNS page; keeping it keeps the editor, and the callback that takes it
  // there, mounted.
  const [kept, setKept] = useState({ id: ruleId, rule: found });

  if (found !== undefined && (found !== kept.rule || ruleId !== kept.id)) {
    setKept({ id: ruleId, rule: found });
  }

  const rule = found ?? (kept.id === ruleId ? kept.rule : undefined);

  useBreadcrumb(rule?.name ?? "DNS rule not found");

  if (rule === undefined) {
    return (
      <FormLayout>
        <PageHeader title="DNS rule not found" />
        <Section title="DNS rule" bodyClassName="p-0">
          <SectionEmpty
            title="This DNS rule does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href="/dns/split" variant="secondary">
                Back to split DNS
              </LinkButton>
            }
          />
        </Section>
      </FormLayout>
    );
  }

  return (
    <FormLayout>
      <PageHeader
        title={rule.name}
        kicker="DNS rule"
        {...(rule.description === "" ? {} : { description: rule.description })}
      />
      <DnsRuleEditor
        key={rule.id}
        rule={rule}
        groups={groups.data?.groups ?? []}
        canEdit={can(me, "dns")}
      />
    </FormLayout>
  );
}
