import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface AppMutations {
  readonly create: Mutation<"post", "/api/v1/apps">;
  readonly update: Mutation<"put", "/api/v1/app/{id}">;
  readonly remove: Mutation<"delete", "/api/v1/app/{id}">;
}

/**
 * Every app mutation. A definition change reaches the connectors as a policy change and moves the
 * routes they advertise, so each one refreshes the machines as well as the app list; errors are
 * left to the caller.
 */
export function useAppMutations(): AppMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/apps", "/api/v1/node");
  };

  return {
    create: api.useMutation("post", "/api/v1/apps", { onSuccess: refresh }),
    update: api.useMutation("put", "/api/v1/app/{id}", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/app/{id}", {
      onSuccess: async () => {
        toast.success("App deleted");
        await refresh();
      },
    }),
  };
}
