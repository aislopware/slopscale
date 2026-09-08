import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Toasty } from "@cloudflare/kumo/components/toast";
import { TooltipProvider } from "@cloudflare/kumo/components/tooltip";
import { LinkProvider } from "@cloudflare/kumo/utils";
import { ArrowCounterClockwiseIcon, WarningCircleIcon } from "@phosphor-icons/react";
import type { ErrorComponentProps } from "@tanstack/react-router";
import { createRootRouteWithContext, Outlet } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { NotFoundPanel } from "~/components/ui/not-found.tsx";
import { toastManager } from "~/components/ui/toast.ts";
import { AppLink } from "~/lib/link.tsx";
import type { RouterContext } from "~/router.tsx";

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
  notFoundComponent: NotFound,
  errorComponent: RootError,
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

/** Only paths outside the app layout land here; inside it the splat route keeps the shell. */
function NotFound(): ReactElement {
  return (
    <div className="flex min-h-dvh items-center justify-center p-6">
      <NotFoundPanel />
    </div>
  );
}

function RootError({ error }: ErrorComponentProps): ReactElement {
  return (
    <div className="flex min-h-dvh items-center justify-center p-6">
      <Empty
        icon={<WarningCircleIcon />}
        title="Something went wrong"
        description={errorMessage(error)}
        contents={
          <Button
            variant="secondary"
            icon={ArrowCounterClockwiseIcon}
            onClick={() => {
              globalThis.location.reload();
            }}
          >
            Reload
          </Button>
        }
      />
    </div>
  );
}
