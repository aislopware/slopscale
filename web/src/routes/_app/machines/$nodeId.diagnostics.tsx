import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { DiagnosticsTab } from "~/components/machines/detail-cards.tsx";
import { useMachinePage } from "~/components/machines/machine-page.ts";

export const Route = createFileRoute("/_app/machines/$nodeId/diagnostics")({
  component: MachineDiagnostics,
});

/** The machine page's Diagnostics tab: the network report, client health and support files. */
function MachineDiagnostics(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();

  return <DiagnosticsTab {...useMachinePage(nodeId, me)} />;
}
