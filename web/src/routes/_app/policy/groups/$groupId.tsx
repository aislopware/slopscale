import { LinkButton } from "@cloudflare/kumo/components/button";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, groupsQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { GroupEditor } from "~/components/access/group-editor.tsx";
import { isBuiltin, isSynced, rulesUsingGroup } from "~/components/access/model.ts";
import { plural } from "~/components/overview/plural.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/groups/$groupId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(groupsQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: GroupPage,
});

function GroupPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { groupId } = Route.useParams();
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { rules } = useSuspenseQuery(accessRulesQuery).data;
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const group = groups.find((candidate) => candidate.id === groupId);

  useBreadcrumb(group?.name ?? "Group not found");

  // The built-in groups are the server's: their members follow the tailnet, not a list.
  if (group === undefined || isBuiltin(group)) {
    return (
      <>
        <PageHeader title={group?.name ?? "Group not found"} docs="ref/access-control" />
        <Section title="Group" bodyClassName="p-0">
          <SectionEmpty
            title={
              group === undefined ? "This group does not exist" : "Built-in groups cannot be edited"
            }
            description={
              group === undefined
                ? "It may have been deleted since the link was made."
                : "The server keeps its members up to date."
            }
            contents={
              <LinkButton href="/policy/groups" variant="secondary">
                Back to groups
              </LinkButton>
            }
          />
        </Section>
      </>
    );
  }

  const used = rulesUsingGroup(rules, group).length;

  return (
    <>
      <PageHeader
        title={group.name}
        kicker={isSynced(group) ? "Group · synced from the identity provider" : "Group"}
        docs="ref/access-control"
        {...(group.description === "" ? {} : { description: group.description })}
        meta={used === 0 ? "No rule names it" : `Named by ${plural(used, "rule")}`}
      />
      <GroupEditor
        key={group.id}
        group={group}
        rules={rules}
        nodes={nodes.data?.nodes ?? []}
        users={users.data?.users ?? []}
        canEdit={can(me, "policy_file")}
      />
    </>
  );
}
