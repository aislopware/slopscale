import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { SettingsTab } from "~/components/machines/detail-cards.tsx";
import { useMachinePage } from "~/components/machines/machine-page.ts";

export const Route = createFileRoute("/_app/machines/$nodeId/settings")({
  component: MachineSettings,
});

/** The machine page's Settings tab: preferences, posture, groups, sharing and the danger zone. */
function MachineSettings(): ReactElement {
  const { me } = Route.useRouteContext();
  const { nodeId } = Route.useParams();

  return <SettingsTab {...useMachinePage(nodeId, me)} />;
}
