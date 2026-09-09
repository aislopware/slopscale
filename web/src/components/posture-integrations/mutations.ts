import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface PostureIntegrationMutations {
  readonly create: Mutation<"post", "/api/v1/posture-integrations">;
  readonly update: Mutation<"put", "/api/v1/posture-integration/{id}">;
  readonly remove: Mutation<"delete", "/api/v1/posture-integration/{id}">;
  readonly sync: Mutation<"post", "/api/v1/posture-integration/{id}/sync">;
  readonly check: Mutation<"post", "/api/v1/posture-integrations/check">;
}

/**
 * Every posture integration mutation, each refreshing the list on success; errors are left to the
 * caller.
 */
export function usePostureIntegrationMutations(): PostureIntegrationMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/posture-integrations", "/api/v1/posture-integration");
  };
  // A sync writes machine attributes, and changing or removing an integration rewrites or drops the
  // ones it wrote, so the machines go stale with the integration.
  const refreshWithNodes = async (): Promise<void> => {
    await invalidate(
      queryClient,
      "/api/v1/posture-integrations",
      "/api/v1/posture-integration",
      "/api/v1/node",
    );
  };

  return {
    create: api.useMutation("post", "/api/v1/posture-integrations", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/posture-integration/{id}", {
      onSuccess: refreshWithNodes,
    }),
    remove: api.useMutation("delete", "/api/v1/posture-integration/{id}", {
      onSuccess: async () => {
        toast.success("Posture integration deleted");
        await refreshWithNodes();
      },
    }),
    sync: api.useMutation("post", "/api/v1/posture-integration/{id}/sync", {
      onSuccess: async () => {
        toast.success("Sync started");
        await refreshWithNodes();
      },
    }),
    check: api.useMutation("post", "/api/v1/posture-integrations/check"),
  };
}
