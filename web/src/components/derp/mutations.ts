import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import type { SetDerpRequestBody } from "~/api/schema.gen.ts";
import { toast } from "~/components/ui/toast.ts";

export interface DerpMutations {
  /** Replaces the whole configuration; every editor sends the full settings. */
  readonly set: Mutation<"put", "/api/v1/derp">;
  readonly reset: Mutation<"delete", "/api/v1/derp">;
  readonly refresh: Mutation<"post", "/api/v1/derp/refresh">;
  /** Sends `next` and toasts `message` on success; the inline controls use it. */
  readonly apply: (next: SetDerpRequestBody, message: string) => void;
}

export function useDerpMutations(): DerpMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/derp", "/api/v1/server");
  };
  const set = api.useMutation("put", "/api/v1/derp", { onSuccess: refresh });
  const reset = api.useMutation("delete", "/api/v1/derp", {
    onSuccess: async () => {
      toast.success("Relay settings reset to the config file");
      await refresh();
    },
  });
  const refetch = api.useMutation("post", "/api/v1/derp/refresh", {
    onSuccess: async () => {
      toast.success("Relay maps refetched");
      await refresh();
    },
    onError: (error) => {
      toast.error("Could not refetch the relay maps", error);
    },
  });

  return {
    set,
    reset,
    refresh: refetch,
    apply: (next, message) => {
      set.mutate(
        { body: next },
        {
          onSuccess: () => {
            toast.success(message);
          },
          onError: (error) => {
            toast.error("Could not change relays", error);
          },
        },
      );
    },
  };
}
