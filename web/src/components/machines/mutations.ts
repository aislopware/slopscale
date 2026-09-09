import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

interface NodeMutations {
  readonly approve: Mutation<"post", "/api/v1/node/{nodeId}/approve">;
  readonly rename: Mutation<"post", "/api/v1/node/{nodeId}/rename/{newName}">;
  readonly setTags: Mutation<"post", "/api/v1/node/{nodeId}/tags">;
  readonly setRoutes: Mutation<"post", "/api/v1/node/{nodeId}/approve_routes">;
  readonly setServices: Mutation<"post", "/api/v1/node/{nodeId}/approve_services">;
  readonly setGlobalExitNode: Mutation<"post", "/api/v1/node/{nodeId}/global-exit-node">;
  readonly expire: Mutation<"post", "/api/v1/node/{nodeId}/expire">;
  readonly suspend: Mutation<"post", "/api/v1/node/{nodeId}/suspend">;
  readonly share: Mutation<"post", "/api/v1/node/{nodeId}/share">;
  readonly unshare: Mutation<"delete", "/api/v1/node/{nodeId}/share/{userId}">;
  readonly remove: Mutation<"delete", "/api/v1/node/{nodeId}">;
}

/**
 * Every node mutation the console performs, each refreshing the node queries on success. Errors are
 * left to the caller so dialogs can show them inline; use `toast.error` where there is no form.
 */
export function useNodeMutations(): NodeMutations {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/node");
  };

  return {
    approve: api.useMutation("post", "/api/v1/node/{nodeId}/approve", {
      onSuccess: async () => {
        toast.success("Machine approved");
        await refresh();
      },
      onError: (error) => {
        toast.error("Could not approve the machine", error);
      },
    }),
    rename: api.useMutation("post", "/api/v1/node/{nodeId}/rename/{newName}", {
      onSuccess: async () => {
        await invalidate(queryClient, "/api/v1/node", "/api/v1/network");
      },
    }),
    setTags: api.useMutation("post", "/api/v1/node/{nodeId}/tags", { onSuccess: refresh }),
    setRoutes: api.useMutation("post", "/api/v1/node/{nodeId}/approve_routes", {
      onSuccess: refresh,
    }),
    // An approval is stored on the node and read back on the service, so both lists go stale.
    setServices: api.useMutation("post", "/api/v1/node/{nodeId}/approve_services", {
      onSuccess: async () => {
        await invalidate(queryClient, "/api/v1/node", "/api/v1/services");
      },
    }),
    setGlobalExitNode: api.useMutation("post", "/api/v1/node/{nodeId}/global-exit-node", {
      onSuccess: async (node) => {
        toast.success(
          node.node.globalExitNode ? "Marked as global exit node" : "No longer a global exit node",
        );
        await refresh();
      },
      onError: (error) => {
        toast.error("Could not change the global exit node", error);
      },
    }),
    expire: api.useMutation("post", "/api/v1/node/{nodeId}/expire", { onSuccess: refresh }),
    suspend: api.useMutation("post", "/api/v1/node/{nodeId}/suspend", { onSuccess: refresh }),
    share: api.useMutation("post", "/api/v1/node/{nodeId}/share", { onSuccess: refresh }),
    unshare: api.useMutation("delete", "/api/v1/node/{nodeId}/share/{userId}", {
      onSuccess: async () => {
        toast.success("Share removed");
        await refresh();
      },
      onError: (error) => {
        toast.error("Could not remove the share", error);
      },
    }),
    remove: api.useMutation("delete", "/api/v1/node/{nodeId}", {
      onSuccess: async () => {
        toast.success("Machine removed");
        await invalidate(
          queryClient,
          "/api/v1/node",
          "/api/v1/group",
          "/api/v1/access-request",
          "/api/v1/network",
        );
        await navigate({ to: "/machines" });
      },
    }),
  };
}
