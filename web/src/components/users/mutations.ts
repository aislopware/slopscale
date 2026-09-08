import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

interface UserMutations {
  readonly create: Mutation<"post", "/api/v1/user">;
  readonly rename: Mutation<"post", "/api/v1/user/{oldId}/rename/{newName}">;
  readonly update: Mutation<"patch", "/api/v1/user/{id}">;
  readonly approve: Mutation<"post", "/api/v1/user/{id}/approve">;
  readonly setRole: Mutation<"post", "/api/v1/user/{id}/role">;
  readonly remove: Mutation<"delete", "/api/v1/user/{id}">;
}

/**
 * Every user mutation the console performs, each refreshing the user queries on success. Errors are
 * left to the caller so dialogs can show them inline; use `toast.error` where there is no form.
 */
export function useUserMutations(): UserMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/user");
  };

  return {
    create: api.useMutation("post", "/api/v1/user", { onSuccess: refresh }),
    rename: api.useMutation("post", "/api/v1/user/{oldId}/rename/{newName}", {
      onSuccess: refresh,
    }),
    update: api.useMutation("patch", "/api/v1/user/{id}", { onSuccess: refresh }),
    approve: api.useMutation("post", "/api/v1/user/{id}/approve", {
      onSuccess: async () => {
        toast.success("User approved");
        await refresh();
      },
      onError: (error) => {
        toast.error("Could not approve user", error);
      },
    }),
    setRole: api.useMutation("post", "/api/v1/user/{id}/role", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/user/{id}", {
      // Deleting a user takes their machines with it, so the node list is stale too.
      onSuccess: async () => {
        toast.success("User deleted");
        await invalidate(queryClient, "/api/v1/user", "/api/v1/node");
      },
    }),
  };
}
