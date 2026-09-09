import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

/**
 * The one settings mutation. Switching an approval off admits everything that waited, so the
 * machines and users refresh along with the settings.
 */
export function useSettingsMutation(): Mutation<"post", "/api/v1/settings"> {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/api/v1/settings", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/settings", "/api/v1/node", "/api/v1/user");
    },
    onError: (error) => {
      toast.error("Could not change the setting", error);
    },
  });
}

/** Switches tailnet lock off. Dropping lock state invalidates the tailnet lock and node queries. */
export function useDisableTailnetLockMutation(): Mutation<"post", "/api/v1/tailnet-lock/disable"> {
  const queryClient = useQueryClient();

  return api.useMutation("post", "/api/v1/tailnet-lock/disable", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/tailnet-lock", "/api/v1/node");
      toast.success("Tailnet lock switched off");
    },
    onError: (error) => {
      toast.error("Could not switch off tailnet lock", error);
    },
  });
}
