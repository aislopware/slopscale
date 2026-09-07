import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface LogStreamMutations {
  readonly create: Mutation<"post", "/api/v1/log-stream">;
  readonly update: Mutation<"put", "/api/v1/log-stream/{id}">;
  readonly test: Mutation<"post", "/api/v1/log-stream/{id}/test">;
  readonly remove: Mutation<"delete", "/api/v1/log-stream/{id}">;
}

/** Every log stream mutation, each refreshing the list on success; errors are left to the caller. */
export function useLogStreamMutations(): LogStreamMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/log-stream");
  };

  return {
    create: api.useMutation("post", "/api/v1/log-stream", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/log-stream/{id}", { onSuccess: refresh }),
    test: api.useMutation("post", "/api/v1/log-stream/{id}/test", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/log-stream/{id}", {
      onSuccess: async () => {
        toast.success("Log stream deleted");
        await refresh();
      },
    }),
  };
}
