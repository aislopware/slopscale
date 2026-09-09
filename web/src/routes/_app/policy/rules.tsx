import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, groupsQuery, posturesQuery } from "~/api/queries.ts";
import { RulesTab } from "~/components/access/rules-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/policy/rules")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(accessRulesQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(posturesQuery),
    ]);
  },
  component: RulesPage,
});

function RulesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { rules, policyFileEnforces } = useSuspenseQuery(accessRulesQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { postures } = useSuspenseQuery(posturesQuery).data;
  const enabled = rules.filter((rule) => rule.enabled).length;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Rules"
        description="Each rule opens ports from a set of sources to a set of destinations. The policy file covers whatever the rules leave out."
        meta={enabled === 1 ? "1 rule enabled" : `${enabled} rules enabled`}
      />
      <RulesTab
        me={me}
        rules={rules}
        groups={groups}
        postures={postures}
        policyFileEnforces={policyFileEnforces}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
