import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can, canSeeMachines, displayName, roleLabel } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { GetStarted } from "~/components/overview/get-started.tsx";
import { MetricTiles } from "~/components/overview/metric-tiles.tsx";
import { NeedsAttention } from "~/components/overview/needs-attention.tsx";
import { QuickActions } from "~/components/overview/quick-actions.tsx";
import { RecentActivity } from "~/components/overview/recent-activity.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const noNodes: readonly Node[] = [];
const noUsers: readonly User[] = [];

export const Route = createFileRoute("/_app/")({
  loader: async ({ context }) => {
    const { me, queryClient } = context;

    await Promise.all([
      canSeeMachines(me) ? queryClient.query(nodesQuery) : Promise.resolve(),
      can(me, "users:read") ? queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: OverviewPage,
});

/** Who is signed in and what bounds them; the page's one line of context. */
function SignedIn({ me }: { readonly me: Me }): ReactElement {
  const role = roleLabel(me);

  return (
    <span>
      Signed in as <span className="font-medium text-kumo-default">{displayName(me)}</span>
      {role === null ? null : ` · ${role}`}
    </span>
  );
}

function OverviewPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const nodes = useQuery({ ...nodesQuery, enabled: canSeeMachines(me) });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [addingMachine, setAddingMachine] = useState(false);
  const nodeList = nodes.data?.nodes;
  const userList = users.data?.users;

  function addMachine(): void {
    setAddingMachine(true);
  }

  return (
    <>
      <PageHeader
        title="Overview"
        meta={<SignedIn me={me} />}
        actions={<QuickActions me={me} onAddMachine={addMachine} />}
      />
      {nodeList?.length === 0 ? <GetStarted me={me} onAddMachine={addMachine} /> : null}
      <MetricTiles
        nodes={nodeList}
        users={userList}
        nodesLoading={nodes.isLoading}
        usersLoading={users.isLoading}
      />
      <NeedsAttention nodes={nodeList ?? noNodes} users={userList ?? noUsers} me={me} />
      <RecentActivity nodes={nodeList ?? noNodes} />
      <CreatePreAuthKeyDialog
        me={me}
        intent="add-machine"
        open={addingMachine}
        onOpenChange={setAddingMachine}
      />
    </>
  );
}
