import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { RoutesTab } from "~/components/machines/detail-cards.tsx";
import { useMachinePage } from "~/components/machines/machine-page.ts";

export const Route = createFileRoute("/_app/machines/$nodeId/routes")({
  component: MachineRoutes,
});

/** The machine page's Routes tab: its routes, the global exit node, app connector and services. */
function MachineRoutes(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();

  return <RoutesTab {...useMachinePage(nodeId, me)} />;
}
