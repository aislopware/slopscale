import { Toasty } from "@cloudflare/kumo/components/toast";
import { TooltipProvider } from "@cloudflare/kumo/components/tooltip";
import { LinkProvider } from "@cloudflare/kumo/utils";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { toastManager } from "~/components/ui/toast.ts";
import { AppLink } from "~/lib/link.tsx";
import type { RouterContext } from "~/router.tsx";

// Errors and unknown addresses fall through to the router's defaults, which draw the same page
// with or without the app layout around it.
export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

function RootLayout(): ReactElement {
  return (
    <LinkProvider component={AppLink}>
      <TooltipProvider delay={300}>
        <Toasty toastManager={toastManager}>
          <Outlet />
        </Toasty>
      </TooltipProvider>
    </LinkProvider>
  );
}
