import type { ReactElement } from "react";

import type { App, Group, Node, Service, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { AppConnectorSection } from "~/components/apps/app-connector-section.tsx";
import { ClientHealthSection } from "~/components/machines/client-health.tsx";
import { ConnectivitySection } from "~/components/machines/connectivity.tsx";
import { DangerZone } from "~/components/machines/danger-zone.tsx";
import { GroupsSection } from "~/components/machines/groups.tsx";
import { OverviewSection } from "~/components/machines/overview.tsx";
import { mayManageNode } from "~/components/machines/owner.ts";
import { PostureSection } from "~/components/machines/posture.tsx";
import { PreferencesSection } from "~/components/machines/preferences.tsx";
import { GlobalExitSection, RoutesSection } from "~/components/machines/routes.tsx";
import { ServicesSection } from "~/components/machines/services.tsx";
import { SharingSection } from "~/components/machines/sharing.tsx";
import { MachineTrafficSection } from "~/components/traffic/machine-section.tsx";

/**
 * The main column of the machine page, in the order a Mesh node page reads: how it connects, what
 * it is, what it routes and serves, how it has been doing, then the settings an operator changes
 * less often, and the danger zone last. Each card is about one thing; the identity is in the rail.
 */
export function MachineCards({
  node,
  me,
  users,
  groups,
  apps,
  services,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  /** Undefined while the caller may not read the policy, which hides the card. */
  readonly groups: readonly Group[] | undefined;
  readonly apps: readonly App[] | undefined;
  readonly services: readonly Service[] | undefined;
}): ReactElement {
  const routes = can(me, "devices:routes");

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <ConnectivitySection node={node} />
      <OverviewSection node={node} />
      <RoutesSection node={node} canEdit={routes} />
      <GlobalExitSection node={node} canEdit={routes} />
      {node.appConnector && apps !== undefined ? (
        <AppConnectorSection node={node} apps={apps} />
      ) : null}
      {services === undefined ? null : (
        <ServicesSection node={node} services={services} canEdit={can(me, "services")} />
      )}
      {can(me, "logs:network:read") ? <MachineTrafficSection nodeId={node.id} /> : null}
      <ClientHealthSection node={node} me={me} />
      <PreferencesSection node={node} me={me} />
      <PostureSection node={node} me={me} />
      {groups === undefined ? null : (
        <GroupsSection node={node} groups={groups} users={users} me={me} />
      )}
      <SharingSection node={node} users={users} me={me} />
      {mayManageNode(me, node) ? (
        <DangerZone node={node} canSuspend={can(me, "devices:core")} />
      ) : null}
    </div>
  );
}
