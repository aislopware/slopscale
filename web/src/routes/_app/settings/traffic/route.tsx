import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Traffic monitor: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/settings/traffic")({
  component: TrafficMonitorLayout,
});

function TrafficMonitorLayout(): ReactElement {
  return <AreaLayout area="/settings/traffic" />;
}
