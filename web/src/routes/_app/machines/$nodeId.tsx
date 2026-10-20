import { cn } from "@cloudflare/kumo/utils";
import { createFileRoute, Outlet, useRouterState } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { appsQuery, groupsQuery, servicesQuery, usersQuery } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import { MachineDetails } from "~/components/machines/details-rail.tsx";
import { MachineTabBar, machineTabOf } from "~/components/machines/header.tsx";
import { useMachinePage } from "~/components/machines/machine-page.ts";
import { MachineSummary } from "~/components/machines/summary-strip.tsx";
import { ClientWarnings } from "~/components/machines/warnings.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { nodeName } from "~/lib/node.ts";

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

/**
 * One machine, as Cloudflare's Mesh draws a node: a bar of tabs (Overview, Routes, Diagnostics,
 * Settings), each its own address, with the machine's actions at its end; the summary strip; then
 * the tab's cards in three columns' room and the details in a sticky rail in the fourth. The strip
 * and the rail stay on every tab. On a phone the rail comes first on Overview, since the name, the
 * ID and the addresses are what an operator opens a machine for; on the other tabs it follows the
 * tab's cards, which are what the reader picked the tab for.
 */
function MachinePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();
  const { node, users } = useMachinePage(nodeId, me);
  const tab = machineTabOf(useRouterState({ select: (state) => state.location.pathname }));

  // The trail names the machine alone; its tags and owner are on the summary strip.
  useBreadcrumb(nodeName(node));

  return (
    <>
      <MachineTabBar node={node} me={me} users={users} tab={tab} />
      <ClientWarnings node={node} />
      <div className="flex flex-col gap-4">
        <MachineSummary node={node} />
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-4">
          <div className="min-w-0 lg:col-span-3">
            <Outlet />
          </div>
          <aside
            aria-label="Machine details"
            className={cn(
              "flex min-w-0 flex-col gap-4 lg:sticky lg:top-[calc(var(--app-header-height,0px)+1rem)]",
              tab === "overview" && "max-lg:order-first",
            )}
          >
            <MachineDetails node={node} me={me} />
          </aside>
        </div>
      </div>
    </>
  );
}
