import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import type { DnsSettings } from "~/api/schema.gen.ts";
import { toast } from "~/components/ui/toast.ts";

export interface DnsMutations {
  /** Replaces the whole configuration; every editor sends the full settings. */
  readonly set: Mutation<"put", "/api/v1/dns">;
  readonly reset: Mutation<"delete", "/api/v1/dns">;
  /** Sends `next` and toasts `message` on success; the inline controls use it. */
  readonly apply: (next: DnsSettings, message: string) => void;
}

export function useDnsMutations(): DnsMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/dns");
  };
  const set = api.useMutation("put", "/api/v1/dns", { onSuccess: refresh });
  const reset = api.useMutation("delete", "/api/v1/dns", {
    onSuccess: async () => {
      toast.success("DNS settings reset to the config file");
      await refresh();
    },
  });

  return {
    set,
    reset,
    apply: (next, message) => {
      set.mutate(
        { body: next },
        {
          onSuccess: () => {
            toast.success(message);
          },
          onError: (error) => {
            toast.error("Could not change DNS", error);
          },
        },
      );
    },
  };
}
