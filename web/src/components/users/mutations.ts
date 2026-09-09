import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { meQuery } from "~/auth/me.ts";
import { toast } from "~/components/ui/toast.ts";

interface UserMutations {
  readonly create: Mutation<"post", "/api/v1/user">;
  readonly rename: Mutation<"post", "/api/v1/user/{oldId}/rename/{newName}">;
  readonly update: Mutation<"patch", "/api/v1/user/{id}">;
  readonly approve: Mutation<"post", "/api/v1/user/{id}/approve">;
  readonly setRole: Mutation<"post", "/api/v1/user/{id}/role">;
  readonly remove: Mutation<"delete", "/api/v1/user/{id}">;
  readonly endSessions: Mutation<"delete", "/api/v1/user/{id}/sessions">;
}

interface InviteMutations {
  readonly create: Mutation<"post", "/api/v1/invite">;
  readonly resend: Mutation<"post", "/api/v1/invite/{id}/resend">;
  readonly revoke: Mutation<"delete", "/api/v1/invite/{id}">;
}

/**
 * Every user mutation the console performs, each refreshing the user queries on success. Errors are
 * left to the caller so dialogs can show them inline; use `toast.error` where there is no form.
 */
export function useUserMutations(): UserMutations {
  const queryClient = useQueryClient();
  const router = useRouter();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/user");
  };
  // A name, profile or role change may be the operator's own, which the account menu and the
  // route guards read from a query held static; fetch it afresh and re-run the guards. Machines
  // carry their owner's profile too.
  const refreshIdentity = async (): Promise<void> => {
    await Promise.all([
      refresh(),
      invalidate(queryClient, "/api/v1/node"),
      queryClient.query({ ...meQuery, staleTime: 0 }),
    ]);
    await router.invalidate();
  };

  return {
    create: api.useMutation("post", "/api/v1/user", { onSuccess: refresh }),
    rename: api.useMutation("post", "/api/v1/user/{oldId}/rename/{newName}", {
      onSuccess: refreshIdentity,
    }),
    update: api.useMutation("patch", "/api/v1/user/{id}", { onSuccess: refreshIdentity }),
    approve: api.useMutation("post", "/api/v1/user/{id}/approve", {
      onSuccess: async () => {
        toast.success("User approved");
        await invalidate(queryClient, "/api/v1/user", "/api/v1/node");
      },
      onError: (error) => {
        toast.error("Could not approve the user", error);
      },
    }),
    setRole: api.useMutation("post", "/api/v1/user/{id}/role", { onSuccess: refreshIdentity }),
    remove: api.useMutation("delete", "/api/v1/user/{id}", {
      // The server refuses to delete a user that still has machines, but it does drop the user's
      // pre-auth keys and group memberships, so those lists are stale too.
      onSuccess: async () => {
        toast.success("User deleted");
        await invalidate(
          queryClient,
          "/api/v1/user",
          "/api/v1/node",
          "/api/v1/preauthkey",
          "/api/v1/group",
        );
      },
    }),
    endSessions: api.useMutation("delete", "/api/v1/user/{id}/sessions", {
      onSuccess: async () => {
        await invalidate(queryClient, "/api/v1/auth/sessions");
      },
    }),
  };
}

/**
 * The invitation mutations, each refreshing the invite list. A created or re-sent invite hands back
 * a link that is shown once, so the caller keeps the result rather than a toast doing it.
 */
export function useInviteMutations(): InviteMutations {
  const queryClient = useQueryClient();
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/invite");
  };

  return {
    create: api.useMutation("post", "/api/v1/invite", { onSuccess: refresh }),
    resend: api.useMutation("post", "/api/v1/invite/{id}/resend", { onSuccess: refresh }),
    revoke: api.useMutation("delete", "/api/v1/invite/{id}", {
      onSuccess: async () => {
        toast.success("Invitation revoked");
        await refresh();
      },
      onError: (error) => {
        toast.error("Could not revoke the invitation", error);
      },
    }),
  };
}
