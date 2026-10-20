import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { OverviewTab } from "~/components/machines/detail-cards.tsx";
import { useMachinePage } from "~/components/machines/machine-page.ts";

export const Route = createFileRoute("/_app/machines/$nodeId/")({
  component: MachineOverview,
});

/** The machine page's Overview tab, the address of the machine itself. */
function MachineOverview(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();

  return <OverviewTab {...useMachinePage(nodeId, me)} />;
}
