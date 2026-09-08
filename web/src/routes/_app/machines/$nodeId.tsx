import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { CheckIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DangerZone } from "~/components/machines/danger-zone.tsx";
import { GroupsSection } from "~/components/machines/groups.tsx";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { AddressesSection, OverviewSection } from "~/components/machines/overview.tsx";
import { PostureSection } from "~/components/machines/posture.tsx";
import { GlobalExitSection, RoutesSection } from "~/components/machines/routes.tsx";
import { SharingSection } from "~/components/machines/sharing.tsx";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { ClientWarnings } from "~/components/machines/warnings.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { isTagged, nodeName, nodeStatus, ownerLabel } from "~/lib/node.ts";

const emptyUsers: readonly User[] = [];

const registerMethods: Record<string, string> = {
  REGISTER_METHOD_AUTH_KEY: "registered with a pre-auth key",
  REGISTER_METHOD_CLI: "registered from the command line",
  REGISTER_METHOD_OIDC: "signed in through the identity provider",
};

export const Route = createFileRoute("/_app/machines/$nodeId")({
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.query(
        api.queryOptions("get", "/api/v1/node/{nodeId}", {
          params: { path: { nodeId: params.nodeId } },
        }),
      ),
      can(context.me, "users:read") ? context.queryClient.query(usersQuery) : Promise.resolve(),
      can(context.me, "policy_file:read")
        ? context.queryClient.query(groupsQuery)
        : Promise.resolve(),
    ]);
  },
  component: MachinePage,
});

function MachinePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();
  const detail = useSuspenseQuery(
    api.queryOptions("get", "/api/v1/node/{nodeId}", { params: { path: { nodeId } } }),
  );
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const { node } = detail.data;
  const userList = users.data?.users ?? emptyUsers;
  const routes = can(me, "devices:routes");

  useBreadcrumb(nodeName(node));

  return (
    <>
      <MachineHeader node={node} me={me} users={userList} />
      <ClientWarnings node={node} />
      <div className="grid items-start gap-6 min-[1200px]:grid-cols-[minmax(0,2fr)_minmax(0,22rem)]">
        <div className="flex flex-col gap-6">
          <OverviewSection node={node} />
          <RoutesSection node={node} canEdit={routes} />
          <PostureSection node={node} me={me} />
          {groups.data === undefined ? null : (
            <GroupsSection node={node} groups={groups.data.groups} users={userList} me={me} />
          )}
          <SharingSection node={node} users={userList} me={me} />
        </div>
        <div className="flex flex-col gap-6">
          <AddressesSection node={node} />
          <GlobalExitSection node={node} canEdit={routes} />
          {can(me, "devices:core") ? <DangerZone node={node} /> : null}
        </div>
      </div>
    </>
  );
}

function MachineHeader({
  node,
  me,
  users,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
}): ReactElement {
  return (
    <PageHeader
      eyebrow={
        <>
          <StatusBadge status={nodeStatus(node)} />
          {node.tags.map((tag) => (
            <Badge key={tag} variant="secondary">
              <span className="font-mono">{tag}</span>
            </Badge>
          ))}
        </>
      }
      title={nodeName(node)}
      meta={<MachineFacts node={node} />}
      actions={
        <>
          {!node.approved && can(me, "devices:core") ? <ApproveButton node={node} /> : null}
          <MachineMenu node={node} me={me} users={users} labelled hideDestructive />
        </>
      }
    />
  );
}

function MachineFacts({ node }: { readonly node: Node }): ReactElement {
  return (
    <>
      <span>{isTagged(node) ? "Tagged machine" : ownerLabel(node)}</span>
      <span aria-hidden>·</span>
      <span>{registerMethods[node.registerMethod] ?? "registered"}</span>
      {node.ephemeral ? (
        <>
          <span aria-hidden>·</span>
          <span>ephemeral, deleted when it logs out or stays offline</span>
        </>
      ) : null}
      <span aria-hidden>·</span>
      <span>
        {node.online ? (
          "connected now"
        ) : (
          <>
            {"last seen "}
            <RelativeTime value={node.lastSeen} />
          </>
        )}
      </span>
    </>
  );
}

function ApproveButton({ node }: { readonly node: Node }): ReactElement {
  const { approve } = useNodeMutations();

  return (
    <Button
      variant="primary"
      icon={CheckIcon}
      loading={approve.isPending}
      onClick={() => {
        approve.mutate({ params: { path: { nodeId: node.id } }, body: {} });
      }}
    >
      Approve
    </Button>
  );
}
