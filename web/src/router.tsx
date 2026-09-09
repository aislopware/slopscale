import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";

import { onSignOut } from "~/auth/session.ts";
import { RouteError, RouteNotFound } from "~/components/ui/error-page.tsx";
import { routeTree } from "~/routeTree.gen.ts";

const staleTime = 15_000;

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime,
      retry: false,
      refetchOnWindowFocus: true,
    },
  },
});

export interface RouterContext {
  readonly queryClient: QueryClient;
}

export const router = createRouter({
  routeTree,
  basepath: "/admin",
  context: { queryClient },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
  // Every route that does not say otherwise shows the same page for an error or an unknown
  // address; the page works out on its own whether the app layout is up around it.
  defaultErrorComponent: RouteError,
  defaultNotFoundComponent: RouteNotFound,
});

// Signing out drops everything the session could see and re-runs the guards.
onSignOut(() => {
  queryClient.clear();
  void router.invalidate();
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
