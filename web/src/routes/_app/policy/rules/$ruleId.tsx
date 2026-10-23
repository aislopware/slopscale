import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, groupsQuery, posturesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { isBuiltinRule } from "~/components/access/model.ts";
import { RuleEditor } from "~/components/access/rule-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/rules/$ruleId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(accessRulesQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(posturesQuery),
    ]);
  },
  component: RulePage,
});

function RulePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { ruleId } = Route.useParams();
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { postures } = useSuspenseQuery(posturesQuery).data;
  const rule = rules.find((candidate) => candidate.id === ruleId);

  useBreadcrumb(rule?.name ?? "Rule not found");

  // A built-in rule belongs to the server: it can be switched off from the list, not rewritten.
  if (rule === undefined || isBuiltinRule(rule)) {
    return (
      <FormLayout width="wide">
        <PageHeader title={rule?.name ?? "Rule not found"} docs="ref/access-control" />
        <Section title="Rule" bodyClassName="p-0">
          <SectionEmpty
            title={
              rule === undefined ? "This rule does not exist" : "Built-in rules cannot be edited"
            }
            description={
              rule === undefined
                ? "It may have been deleted since the link was made."
                : "The server keeps this rule. Switch it off from the list if the tailnet should not have it."
            }
            contents={
              <LinkButton href="/policy/rules" variant="secondary">
                Back to rules
              </LinkButton>
            }
          />
        </Section>
      </FormLayout>
    );
  }

  return (
    <FormLayout width="wide">
      <PageHeader
        title={rule.name}
        kicker="Rule"
        docs="ref/access-control"
        {...(rule.description === "" ? {} : { description: rule.description })}
      />
      <RuleEditor
        key={rule.id}
        rule={rule}
        rules={rules}
        groups={groups}
        postures={postures}
        policyFileEnforces={policyFileEnforces}
        canEdit={can(me, "policy_file")}
      />
    </FormLayout>
  );
}
