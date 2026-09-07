import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface DnsRuleMutations {
  readonly create: Mutation<"post", "/api/v1/dns/rule">;
  readonly update: Mutation<"put", "/api/v1/dns/rule/{id}">;
  readonly remove: Mutation<"delete", "/api/v1/dns/rule/{id}">;
}

/** Every group DNS rule mutation, each refreshing the rule list on success. */
export function useDnsRuleMutations(): DnsRuleMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/dns/rule");
  };

  return {
    create: api.useMutation("post", "/api/v1/dns/rule", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/dns/rule/{id}", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/dns/rule/{id}", {
      onSuccess: async () => {
        toast.success("DNS rule deleted");
        await refresh();
      },
    }),
  };
}
