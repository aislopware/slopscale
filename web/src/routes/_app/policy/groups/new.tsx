import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { GroupEditor } from "~/components/access/group-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/groups/new")({
  loader: async ({ context }) => {
    await context.queryClient.query(accessRulesQuery);
  },
  component: NewGroupPage,
});

function NewGroupPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { rules } = useSuspenseQuery(accessRulesQuery).data;
  // Membership names machines and users; a caller without those scopes picks from nothing.
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });

  useBreadcrumb("New group");

  return (
    <FormLayout>
      <PageHeader
        title="New group"
        description="A set of machines: add users to include every machine they own, or add machines on their own."
      />
      <GroupEditor
        rules={rules}
        nodes={nodes.data?.nodes ?? []}
        users={users.data?.users ?? []}
        canEdit={can(me, "policy_file")}
      />
    </FormLayout>
  );
}
