import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

/**
 * Changes the traffic settings. DNS logging moves every client's resolvers, so the DNS page's view
 * of them refreshes along with the gateways'. A refusal is a toast unless the caller shows it where
 * the control is.
 */
export function useTrafficSettingsMutation({
  quiet = false,
}: {
  /** Leaves a refusal to the caller's own `onError` instead of a toast. */
  readonly quiet?: boolean;
} = {}): Mutation<"patch", "/api/v1/traffic/settings"> {
  const queryClient = useQueryClient();

  return api.useMutation("patch", "/api/v1/traffic/settings", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/traffic", "/api/v1/dns");
    },
    onError: (error) => {
      if (!quiet) {
        toast.error("Could not change the traffic settings", error);
      }
    },
  });
}

/** Forgets a gateway's agent; errors are shown in the confirmation dialog. */
export function useForgetGatewayMutation(): Mutation<
  "delete",
  "/api/v1/traffic/reporters/{nodeId}"
> {
  const queryClient = useQueryClient();

  return api.useMutation("delete", "/api/v1/traffic/reporters/{nodeId}", {
    onSuccess: async () => {
      toast.success("Gateway forgotten");
      await invalidate(queryClient, "/api/v1/traffic", "/api/v1/dns");
    },
  });
}
