import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface WebhookMutations {
  readonly create: Mutation<"post", "/api/v1/webhook">;
  readonly update: Mutation<"put", "/api/v1/webhook/{id}">;
  readonly rotate: Mutation<"post", "/api/v1/webhook/{id}/rotate">;
  readonly test: Mutation<"post", "/api/v1/webhook/{id}/test">;
  readonly remove: Mutation<"delete", "/api/v1/webhook/{id}">;
}

/** Every webhook mutation, each refreshing the list on success; errors are left to the caller. */
export function useWebhookMutations(): WebhookMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/webhook");
  };

  return {
    create: api.useMutation("post", "/api/v1/webhook", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/webhook/{id}", { onSuccess: refresh }),
    rotate: api.useMutation("post", "/api/v1/webhook/{id}/rotate", { onSuccess: refresh }),
    test: api.useMutation("post", "/api/v1/webhook/{id}/test", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/webhook/{id}", {
      onSuccess: async () => {
        toast.success("Webhook deleted");
        await refresh();
      },
    }),
  };
}
