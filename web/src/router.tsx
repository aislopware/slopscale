import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";

import { onSessionEnd } from "~/auth/ended.ts";
import { RouteError, RouteNotFound } from "~/components/ui/error-page.tsx";
import { routeTree } from "~/routeTree.gen.ts";

const staleTime = 15_000;

/** How many times a request the browser never got an answer to is sent again before it fails. */
const networkRetries = 1;

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime,
      // A refused request is an answer and is shown as one; a dropped connection, which fetch
      // reports as a TypeError, is tried once more before the page says the server did not answer.
      retry: (failureCount, error): boolean =>
        error instanceof TypeError && failureCount < networkRetries,
      refetchOnWindowFocus: true,
    },
  },
});

export interface RouterContext {
  readonly queryClient: QueryClient;
}

export const router = createRouter({
  routeTree,
  basepath: "/console",
  context: { queryClient },
  defaultPreload: "intent",
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
  // Every route that does not say otherwise shows the same page for an error or an unknown
  // address; the page works out on its own whether the app layout is up around it.
  defaultErrorComponent: RouteError,
  defaultNotFoundComponent: RouteNotFound,
});

// The end of the session drops everything it could see and re-runs the guards.
onSessionEnd(() => {
  queryClient.clear();
  void router.invalidate();
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
