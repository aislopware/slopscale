import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Integrations: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/integrations")({
  component: IntegrationsLayout,
});

function IntegrationsLayout(): ReactElement {
  return <AreaLayout area="/integrations" />;
}
