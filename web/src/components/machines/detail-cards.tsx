import type { ReactElement } from "react";

import type { App, Group, Node, Service, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { AppConnectorSection } from "~/components/apps/app-connector-section.tsx";
import {
  ClientHealthSection,
  DiagnosticFilesSection,
} from "~/components/machines/client-health.tsx";
import { ConnectivitySection } from "~/components/machines/connectivity.tsx";
import { DangerZone } from "~/components/machines/danger-zone.tsx";
import { GroupsSection } from "~/components/machines/groups.tsx";
import { OverviewSection } from "~/components/machines/overview.tsx";
import { mayManageNode } from "~/components/machines/owner.ts";
import { PostureSection } from "~/components/machines/posture.tsx";
import { PreferencesSection } from "~/components/machines/preferences.tsx";
import { RoutesPreview } from "~/components/machines/routes-preview.tsx";
import { GlobalExitSection, RoutesSection } from "~/components/machines/routes.tsx";
import { ServicesSection } from "~/components/machines/services.tsx";
import { SharingSection } from "~/components/machines/sharing.tsx";
import { MachineTrafficSection } from "~/components/traffic/machine-section.tsx";

/** What every tab of the machine page is drawn from. */
export interface MachineTabProps {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  /** Undefined while the caller may not read the policy, which hides the card. */
  readonly groups: readonly Group[] | undefined;
  readonly apps: readonly App[] | undefined;
  readonly services: readonly Service[] | undefined;
}

const columnClass = "flex min-w-0 flex-col gap-4";

/**
 * The Overview tab, as a Mesh node's: how the machine connects and what it is, then compact looks
 * at what it routes and how much went through the gateways, each with the way to its full tab or
 * page. Nothing on it is the only place a thing can be changed.
 */
export function OverviewTab({ node, me }: MachineTabProps): ReactElement {
  return (
    <div className={columnClass}>
      <ConnectivitySection node={node} preview />
      <OverviewSection node={node} />
      <RoutesPreview node={node} />
      {can(me, "logs:network:read") ? <MachineTrafficSection nodeId={node.id} /> : null}
    </div>
  );
}

/** The Routes tab: what the machine routes for the tailnet and the services it hosts. */
export function RoutesTab({ node, me, apps, services }: MachineTabProps): ReactElement {
  const routes = can(me, "devices:routes");

  return (
    <div className={columnClass}>
      <RoutesSection node={node} canEdit={routes} />
      <GlobalExitSection node={node} canEdit={routes} />
      {node.appConnector && apps !== undefined ? (
        <AppConnectorSection node={node} apps={apps} />
      ) : null}
      {services === undefined ? null : (
        <ServicesSection node={node} services={services} canEdit={can(me, "services")} />
      )}
    </div>
  );
}

/**
 * The Diagnostics tab: the client's whole network report, what it says about its own health, and
 * the files it hands over for a support request.
 */
export function DiagnosticsTab({ node, me }: MachineTabProps): ReactElement {
  return (
    <div className={columnClass}>
      <ConnectivitySection node={node} />
      <ClientHealthSection node={node} />
      <DiagnosticFilesSection node={node} me={me} />
    </div>
  );
}

/**
 * The Settings tab, the settings an operator changes less often, in the order of a dashboard
 * settings page: the client's preferences, posture, who it is grouped with and shared with, and the
 * danger zone last.
 */
export function SettingsTab({ node, me, users, groups }: MachineTabProps): ReactElement {
  return (
    <div className={columnClass}>
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
