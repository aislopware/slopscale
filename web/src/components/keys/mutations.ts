import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";

interface PreAuthKeyMutations {
  readonly create: Mutation<"post", "/api/v1/preauthkey">;
  readonly expire: Mutation<"post", "/api/v1/preauthkey/expire">;
  readonly remove: Mutation<"delete", "/api/v1/preauthkey">;
}

/**
 * Pre-auth key mutations, each refreshing the key list on success. Errors are left to the caller so
 * dialogs can show them inline.
 */
export function usePreAuthKeyMutations(): PreAuthKeyMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/preauthkey");
  };

  return {
    create: api.useMutation("post", "/api/v1/preauthkey", { onSuccess: refresh }),
    expire: api.useMutation("post", "/api/v1/preauthkey/expire", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/preauthkey", { onSuccess: refresh }),
  };
}

interface ApiKeyMutations {
  readonly create: Mutation<"post", "/api/v1/apikey">;
  readonly expire: Mutation<"post", "/api/v1/apikey/expire">;
  readonly remove: Mutation<"delete", "/api/v1/apikey/{prefix}">;
}

/** API key mutations; the same shape as the pre-auth ones, on the API key collection. */
export function useApiKeyMutations(): ApiKeyMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/apikey");
  };

  return {
    create: api.useMutation("post", "/api/v1/apikey", { onSuccess: refresh }),
    expire: api.useMutation("post", "/api/v1/apikey/expire", { onSuccess: refresh }),
    remove: api.useMutation("delete", "/api/v1/apikey/{prefix}", { onSuccess: refresh }),
  };
}
