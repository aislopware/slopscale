import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

export interface AccessMutations {
  readonly createGroup: Mutation<"post", "/api/v1/group">;
  readonly updateGroup: Mutation<"patch", "/api/v1/group/{id}">;
  readonly deleteGroup: Mutation<"delete", "/api/v1/group/{id}">;
  readonly addMember: Mutation<"post", "/api/v1/group/{id}/member">;
  readonly removeNode: Mutation<"delete", "/api/v1/group/{id}/node/{nodeId}">;
  readonly removeUser: Mutation<"delete", "/api/v1/group/{id}/user/{userId}">;
  readonly createRule: Mutation<"post", "/api/v1/access-rule">;
  readonly updateRule: Mutation<"put", "/api/v1/access-rule/{id}">;
  readonly setRuleEnabled: Mutation<"patch", "/api/v1/access-rule/{id}">;
  readonly deleteRule: Mutation<"delete", "/api/v1/access-rule/{id}">;
  readonly createPosture: Mutation<"post", "/api/v1/posture">;
  readonly updatePosture: Mutation<"put", "/api/v1/posture/{id}">;
  readonly deletePosture: Mutation<"delete", "/api/v1/posture/{id}">;
  readonly createRequest: Mutation<"post", "/api/v1/access-request">;
  readonly approveRequest: Mutation<"post", "/api/v1/access-request/{id}/approve">;
  readonly denyRequest: Mutation<"post", "/api/v1/access-request/{id}/deny">;
  readonly cancelRequest: Mutation<"delete", "/api/v1/access-request/{id}">;
}

/**
 * Every group, rule and posture mutation, each refreshing the group and rule queries on success.
 * Errors are left to the caller so dialogs can show them inline; the inline switches toast
 * instead.
 */
export function useAccessMutations(): AccessMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(
      queryClient,
      "/api/v1/group",
      "/api/v1/access-rule",
      "/api/v1/posture",
      "/api/v1/access-request",
      "/api/v1/network",
      // Rules, groups and postures are what the graph is derived from.
      "/api/v1/access-graph",
    );
  };

  return {
    createGroup: api.useMutation("post", "/api/v1/group", { onSuccess: refresh }),
    updateGroup: api.useMutation("patch", "/api/v1/group/{id}", { onSuccess: refresh }),
    deleteGroup: api.useMutation("delete", "/api/v1/group/{id}", {
      onSuccess: async () => {
        toast.success("Group deleted");
        await refresh();
      },
    }),
    addMember: api.useMutation("post", "/api/v1/group/{id}/member", { onSuccess: refresh }),
    removeNode: api.useMutation("delete", "/api/v1/group/{id}/node/{nodeId}", {
      onSuccess: refresh,
    }),
    removeUser: api.useMutation("delete", "/api/v1/group/{id}/user/{userId}", {
      onSuccess: refresh,
    }),
    createRule: api.useMutation("post", "/api/v1/access-rule", { onSuccess: refresh }),
    updateRule: api.useMutation("put", "/api/v1/access-rule/{id}", { onSuccess: refresh }),
    setRuleEnabled: api.useMutation("patch", "/api/v1/access-rule/{id}", { onSuccess: refresh }),
    deleteRule: api.useMutation("delete", "/api/v1/access-rule/{id}", {
      onSuccess: async () => {
        toast.success("Rule deleted");
        await refresh();
      },
    }),
    createPosture: api.useMutation("post", "/api/v1/posture", { onSuccess: refresh }),
    updatePosture: api.useMutation("put", "/api/v1/posture/{id}", { onSuccess: refresh }),
    deletePosture: api.useMutation("delete", "/api/v1/posture/{id}", {
      onSuccess: async () => {
        toast.success("Posture deleted");
        await refresh();
      },
    }),
    createRequest: api.useMutation("post", "/api/v1/access-request", { onSuccess: refresh }),
    approveRequest: api.useMutation("post", "/api/v1/access-request/{id}/approve", {
      onSuccess: async () => {
        toast.success("Request approved");
        await refresh();
      },
    }),
    denyRequest: api.useMutation("post", "/api/v1/access-request/{id}/deny", {
      onSuccess: async () => {
        toast.success("Request denied");
        await refresh();
      },
    }),
    cancelRequest: api.useMutation("delete", "/api/v1/access-request/{id}", {
      onSuccess: async () => {
        toast.success("Request withdrawn");
        await refresh();
      },
    }),
  };
}
