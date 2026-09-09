import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface ServiceMutations {
  readonly create: Mutation<"post", "/api/v1/services">;
  readonly update: Mutation<"put", "/api/v1/service/{name}">;
  readonly remove: Mutation<"delete", "/api/v1/service/{name}">;
  readonly setServices: Mutation<"post", "/api/v1/node/{nodeId}/approve_services">;
}

/**
 * Every service mutation plus the per-machine approval, each refreshing the services and the
 * machines on success, because an approval is stored on the machine and read back on the service.
 * Errors are left to the caller so dialogs can show them inline.
 */
export function useServiceMutations(): ServiceMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/services", "/api/v1/node");
  };

  return {
    create: api.useMutation("post", "/api/v1/services", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/service/{name}", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/service/{name}", {
      onSuccess: async () => {
        toast.success("Service deleted");
        await refresh();
      },
    }),
    setServices: api.useMutation("post", "/api/v1/node/{nodeId}/approve_services", {
      onSuccess: refresh,
    }),
  };
}
