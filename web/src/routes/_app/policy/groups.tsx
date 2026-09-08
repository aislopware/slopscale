import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, groupsQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { GroupsTab } from "~/components/access/groups-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/policy/groups")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(groupsQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: GroupsPage,
});

function GroupsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { rules } = useSuspenseQuery(accessRulesQuery).data;
  // Group membership names machines and users; a caller without those scopes still sees counts.
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Groups"
        description="Named sets of users, machines and tags. Rules and networks refer to groups, so membership is the one place to change who is covered."
        meta={groups.length === 1 ? "1 group" : `${groups.length} groups`}
      />
      <GroupsTab
        me={me}
        groups={groups}
        rules={rules}
        nodes={nodes.data?.nodes}
        users={users.data?.users}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
