import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Access controls: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/policy")({
  component: PolicyLayout,
});

function PolicyLayout(): ReactElement {
  return <AreaLayout area="/policy" />;
}
