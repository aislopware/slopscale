import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, groupsQuery, posturesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { RuleEditor } from "~/components/access/rule-editor.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/rules/new")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(accessRulesQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(posturesQuery),
    ]);
  },
  component: NewRulePage,
});

function NewRulePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { postures } = useSuspenseQuery(posturesQuery).data;

  useBreadcrumb("New rule");

  return (
    <>
      <PageHeader
        title="New rule"
        docs="ref/access-control"
        description="Pick who opens the connection, what it may reach and what their machine must meet."
      />
      <RuleEditor
        rules={rules}
        groups={groups}
        postures={postures}
        policyFileEnforces={policyFileEnforces}
        canEdit={can(me, "policy_file")}
      />
    </>
  );
}
