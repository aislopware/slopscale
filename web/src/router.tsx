import { QueryClient } from "@tanstack/react-query";
import { createRouter } from "@tanstack/react-router";

import { session } from "~/auth/session.ts";
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
});

// Signing in or out (or a 401 clearing the key) re-runs the guards.
session.subscribe(() => {
  queryClient.clear();
  void router.invalidate();
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
