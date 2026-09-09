import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRequestsQuery, groupsQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { pendingCount } from "~/components/access/request-model.ts";
import { RequestsTab } from "~/components/access/requests-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/policy/requests")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(accessRequestsQuery),
      context.queryClient.query(groupsQuery),
    ]);
  },
  component: RequestsPage,
});

function RequestsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { requests, canDecide } = useSuspenseQuery(accessRequestsQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  // A request names a user and machines; a caller without those scopes still sees the request.
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const pending = pendingCount(requests);

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Requests"
        description="Requests to join a group for a set time. An approver decides here, and access ends when the time is up."
        meta={pending === 1 ? "1 waiting for a decision" : `${pending} waiting for a decision`}
      />
      <RequestsTab
        me={me}
        requests={requests}
        canDecide={canDecide}
        groups={groups}
        users={users.data?.users}
        nodes={nodes.data?.nodes}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
