import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Traffic: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/traffic")({
  component: TrafficLayout,
});

function TrafficLayout(): ReactElement {
  return <AreaLayout area="/traffic" />;
}
