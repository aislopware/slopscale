import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, policyQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PolicyFileTab } from "~/components/policy/policy-file-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";

export const Route = createFileRoute("/_app/policy/file")({
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(policyQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: PolicyFilePage,
});

function PolicyFilePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const policy = useSuspenseQuery(policyQuery).data;
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;

  return (
    <>
      <PageHeader
        title="Policy file"
        description="The HuJSON policy for everything the rules do not say. Check validates a draft against the server without saving it."
      />
      <PolicyFileTab
        policy={policy}
        canEdit={can(me, "policy_file")}
        hasRules={rules.some((rule) => rule.enabled)}
        enforces={policyFileEnforces}
      />
    </>
  );
}
