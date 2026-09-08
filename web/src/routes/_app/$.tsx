import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { NotFoundPanel } from "~/components/ui/not-found.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

/**
 * Every path the console does not know, matched inside the app layout so the "not found" page keeps
 * the sidebar and the operator can simply click elsewhere.
 */
export const Route = createFileRoute("/_app/$")({
  component: NotFound,
});

function NotFound(): ReactElement {
  useBreadcrumb("Page not found");

  return (
    <div className="flex flex-1 items-center justify-center p-6">
      <NotFoundPanel />
    </div>
  );
}
