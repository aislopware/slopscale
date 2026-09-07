import { Banner } from "@cloudflare/kumo/components/banner";
import { Button } from "@cloudflare/kumo/components/button";
import { ArrowLeftIcon, CheckIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { AddressesCard, OverviewCard } from "~/components/machines/overview-card.tsx";
import { GlobalExitCard, RoutesCard } from "~/components/machines/routes-card.tsx";
import { SharingCard } from "~/components/machines/sharing-card.tsx";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { nodeName, nodeStatus } from "~/lib/node.ts";

const emptyUsers: readonly User[] = [];

export const Route = createFileRoute("/_app/machines/$nodeId")({
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.query(
        api.queryOptions("get", "/api/v1/node/{nodeId}", {
          params: { path: { nodeId: params.nodeId } },
        }),
      ),
      can(context.me, "users:read") ? context.queryClient.query(usersQuery) : Promise.resolve(),
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
  const { node } = detail.data;
  const userList = users.data?.users ?? emptyUsers;

  return (
    <>
      <div>
        <Link
          to="/machines"
          className="inline-flex items-center gap-1.5 text-kumo-subtle hover:text-kumo-default"
        >
          <ArrowLeftIcon />
          Machines
        </Link>
      </div>
      <MachineHeader node={node} me={me} users={userList} />
      {node.approved ? null : (
        <Banner
          variant="alert"
          icon={<WarningCircleIcon />}
          title="Waiting for approval"
          description="This machine cannot reach the tailnet until an administrator approves it."
        />
      )}
      <div className="grid gap-6 lg:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-6">
          <OverviewCard node={node} />
          <RoutesCard node={node} canEdit={can(me, "devices:routes")} />
          <SharingCard node={node} users={userList} me={me} />
        </div>
        <div className="flex flex-col gap-6">
          <AddressesCard node={node} />
          <GlobalExitCard node={node} canEdit={can(me, "devices:routes")} />
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
  const status = nodeStatus(node);

  return (
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="flex flex-col gap-1.5">
        <h1 className="text-xl font-semibold text-kumo-default">{nodeName(node)}</h1>
        <div className="flex flex-wrap items-center gap-2 text-kumo-subtle">
          <StatusBadge status={status} />
          {node.online ? null : (
            <span>
              {"last seen "}
              <RelativeTime value={node.lastSeen} />
            </span>
          )}
        </div>
      </div>
      <div className="flex items-center gap-2">
        {!node.approved && can(me, "devices:core") ? <ApproveButton node={node} /> : null}
        <MachineMenu node={node} me={me} users={users} labelled />
      </div>
    </div>
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
