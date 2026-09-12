import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { appsQuery, groupsQuery, servicesQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import { AppConnectorSection } from "~/components/apps/app-connector-section.tsx";
import { ClientHealthSection } from "~/components/machines/client-health.tsx";
import { ConnectivitySection } from "~/components/machines/connectivity.tsx";
import { DangerZone } from "~/components/machines/danger-zone.tsx";
import { GroupsSection } from "~/components/machines/groups.tsx";
import { MachineHeader } from "~/components/machines/header.tsx";
import { AddressesSection, OverviewSection } from "~/components/machines/overview.tsx";
import { mayManageNode } from "~/components/machines/owner.ts";
import { machinePolling } from "~/components/machines/polling.ts";
import { PostureSection } from "~/components/machines/posture.tsx";
import { PreferencesSection } from "~/components/machines/preferences.tsx";
import { GlobalExitSection, RoutesSection } from "~/components/machines/routes.tsx";
import { ServicesSection } from "~/components/machines/services.tsx";
import { SharingSection } from "~/components/machines/sharing.tsx";
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
  const routes = can(me, "devices:routes");

  useBreadcrumb(nodeName(node));

  return (
    <>
      <MachineHeader node={node} me={me} users={userList} />
      <ClientWarnings node={node} />
      <div className="grid grid-cols-1 items-start gap-6 min-[1200px]:grid-cols-[minmax(0,2fr)_minmax(0,22rem)]">
        <div className="flex flex-col gap-6">
          <OverviewSection node={node} />
          <RoutesSection node={node} canEdit={routes} />
          {node.appConnector && apps.data !== undefined ? (
            <AppConnectorSection node={node} apps={apps.data.apps} />
          ) : null}
          {services.data === undefined ? null : (
            <ServicesSection
              node={node}
              services={services.data.services}
              canEdit={can(me, "services")}
            />
          )}
          <PreferencesSection node={node} me={me} />
          <PostureSection node={node} me={me} />
          {groups.data === undefined ? null : (
            <GroupsSection node={node} groups={groups.data.groups} users={userList} me={me} />
          )}
          <SharingSection node={node} users={userList} me={me} />
        </div>
        <div className="flex flex-col gap-6">
          <AddressesSection node={node} />
          <ConnectivitySection node={node} />
          <ClientHealthSection node={node} me={me} />
          <GlobalExitSection node={node} canEdit={routes} />
          {mayManageNode(me, node) ? (
            <DangerZone node={node} canSuspend={can(me, "devices:core")} />
          ) : null}
        </div>
      </div>
    </>
  );
}
