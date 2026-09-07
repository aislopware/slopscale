import { useQueryClient } from "@tanstack/react-query";

import { api } from "~/api/client.ts";
import type { Mutation } from "~/api/mutation.ts";
import { invalidate } from "~/api/queries.ts";
import { toast } from "~/components/ui/toast.ts";

/** Deleting a recording refreshes the list; errors are left to the dialog. */
export function useDeleteRecording(): Mutation<"delete", "/api/v1/ssh-recording/{id}"> {
  const queryClient = useQueryClient();

  return api.useMutation("delete", "/api/v1/ssh-recording/{id}", {
    onSuccess: async () => {
      toast.success("Recording deleted");
      await invalidate(queryClient, "/api/v1/ssh-recording");
    },
  });
}
