import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { appsQuery, groupsQuery, servicesQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import { MachineCards } from "~/components/machines/detail-cards.tsx";
import { MachineDetails } from "~/components/machines/details-rail.tsx";
import { MachineHeader } from "~/components/machines/header.tsx";
import { machinePolling } from "~/components/machines/polling.ts";
import { MachineSummary } from "~/components/machines/summary-strip.tsx";
import { ClientWarnings } from "~/components/machines/warnings.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { nodeName } from "~/lib/node.ts";

const emptyUsers: readonly User[] = [];

export const Route = createFileRoute("/_app/machines/$nodeId")({
  loader: async ({ context, params }) => {
    await Promise.all([
      context.queryClient.query(
        api.queryOptions("get", "/api/v1/node/{nodeId}", {
          params: { path: { nodeId: params.nodeId } },
        }),
      ),
      canListUsers(context.me) ? context.queryClient.query(usersQuery) : Promise.resolve(),
      can(context.me, "policy_file:read")
        ? context.queryClient.query(groupsQuery)
        : Promise.resolve(),
      can(context.me, "policy_file:read")
        ? context.queryClient.query(appsQuery)
        : Promise.resolve(),
      can(context.me, "services:read")
        ? context.queryClient.query(servicesQuery)
        : Promise.resolve(),
    ]);
  },
  component: MachinePage,
});

function MachinePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();
  const detail = useSuspenseQuery({
    ...api.queryOptions("get", "/api/v1/node/{nodeId}", { params: { path: { nodeId } } }),
    ...machinePolling,
  });
  const users = useQuery({ ...usersQuery, enabled: canListUsers(me) });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const apps = useQuery({ ...appsQuery, enabled: can(me, "policy_file:read") });
  const services = useQuery({ ...servicesQuery, enabled: can(me, "services:read") });
  const { node } = detail.data;
  const userList = users.data?.users ?? emptyUsers;

  // The trail names the machine alone; its tags and owner are on the summary strip.
  useBreadcrumb(nodeName(node));

  return (
    <>
      <MachineHeader node={node} me={me} users={userList} />
      <ClientWarnings node={node} />
      {/* The Zero Trust detail layout: the summary strip, then the cards in three columns' room
          and the details in a sticky rail in the fourth. On a phone the rail comes first, since
          the name, the ID and the addresses are what an operator opens a machine for. */}
      <div className="flex flex-col gap-4">
        <MachineSummary node={node} />
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-4">
          <div className="min-w-0 lg:col-span-3">
            <MachineCards
              node={node}
              me={me}
              users={userList}
              groups={groups.data?.groups}
              apps={apps.data?.apps}
              services={services.data?.services}
            />
          </div>
          <aside
            aria-label="Machine details"
            className="flex min-w-0 flex-col gap-4 max-lg:order-first lg:sticky lg:top-[calc(var(--app-header-height,0px)+1rem)]"
          >
            <MachineDetails node={node} me={me} />
          </aside>
        </div>
      </div>
    </>
  );
}
