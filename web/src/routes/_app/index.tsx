import { LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { DevicesIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { nodesQuery, settingsQuery, usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can, greeting } from "~/auth/me.ts";
import { ApprovalSummary } from "~/components/overview/approval-summary.tsx";
import { PendingCard } from "~/components/overview/pending-card.tsx";
import { RecentCard } from "~/components/overview/recent-card.tsx";
import { StatsGrid } from "~/components/overview/stats-grid.tsx";
import { Card, CardBody } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const noNodes: readonly Node[] = [];
const noUsers: readonly User[] = [];

/** Sized for a card-width empty state rather than a full page. */
const emptyIconSize = 40;

export const Route = createFileRoute("/_app/")({
  loader: async ({ context }) => {
    const { me, queryClient } = context;

    await Promise.all([
      can(me, "devices:core:read") ? queryClient.query(nodesQuery) : Promise.resolve(),
      can(me, "users:read") ? queryClient.query(usersQuery) : Promise.resolve(),
      can(me, "feature_settings:read") ? queryClient.query(settingsQuery) : Promise.resolve(),
    ]);
  },
  component: OverviewPage,
});

function NoMachines({ canCreateKeys }: { readonly canCreateKeys: boolean }): ReactElement {
  return (
    <Card>
      <CardBody>
        <Empty
          icon={<DevicesIcon size={emptyIconSize} />}
          title="No machines yet"
          description="Register a device with a pre-auth key or by signing in; it appears here immediately."
          {...(canCreateKeys
            ? {
                contents: (
                  <LinkButton href="/keys" variant="secondary" size="sm">
                    Create a pre-auth key
                  </LinkButton>
                ),
              }
            : {})}
        />
      </CardBody>
    </Card>
  );
}

function OverviewPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const settings = useQuery({ ...settingsQuery, enabled: can(me, "feature_settings:read") });
  const nodeList = nodes.data?.nodes;
  const userList = users.data?.users;

  return (
    <>
      <PageHeader title="Overview" description={greeting(me)} />
      <StatsGrid
        nodes={nodeList}
        users={userList}
        nodesLoading={nodes.isLoading}
        usersLoading={users.isLoading}
      />
      <div className="grid items-start gap-6 lg:grid-cols-[2fr_1fr]">
        <div className="flex flex-col gap-6">
          {nodeList?.length === 0 ? <NoMachines canCreateKeys={can(me, "auth_keys")} /> : null}
          <PendingCard nodes={nodeList ?? noNodes} users={userList ?? noUsers} me={me} />
          <RecentCard nodes={nodeList ?? noNodes} />
        </div>
        {settings.data === undefined ? null : <ApprovalSummary settings={settings.data} />}
      </div>
    </>
  );
}
