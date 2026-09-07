import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface NetworkMutations {
  readonly create: Mutation<"post", "/api/v1/network">;
  readonly update: Mutation<"put", "/api/v1/network/{id}">;
  readonly setEnabled: Mutation<"patch", "/api/v1/network/{id}">;
  readonly remove: Mutation<"delete", "/api/v1/network/{id}">;
  readonly setRoutes: Mutation<"post", "/api/v1/node/{nodeId}/approve_routes">;
}

/**
 * Every network mutation plus the per-machine route approval, each refreshing the networks and the
 * machines on success, because a network change approves or withdraws routes on its routers. Errors
 * are left to the caller so dialogs can show them inline.
 */
export function useNetworkMutations(): NetworkMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/network", "/api/v1/node");
  };

  return {
    create: api.useMutation("post", "/api/v1/network", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/network/{id}", { onSuccess: refresh }),
    setEnabled: api.useMutation("patch", "/api/v1/network/{id}", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/network/{id}", {
      onSuccess: async () => {
        toast.success("Network deleted");
        await refresh();
      },
    }),
    setRoutes: api.useMutation("post", "/api/v1/node/{nodeId}/approve_routes", {
      onSuccess: refresh,
    }),
  };
}
