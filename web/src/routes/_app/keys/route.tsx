import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { AreaLayout } from "~/components/layout/sub-nav.tsx";

/** Keys: its pages under the sub-navigation bar that links them. */
export const Route = createFileRoute("/_app/keys")({
  component: KeysLayout,
});

function KeysLayout(): ReactElement {
  return <AreaLayout area="/keys" />;
}
