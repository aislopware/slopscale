import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { UseMutationResult } from "@tanstack/react-query";
import { useState } from "react";

import { api, fetchClient } from "~/api/client.ts";
import { ApiError } from "~/api/error.ts";
import type { Mutation } from "~/api/mutation.ts";
import { derpQuery, invalidate } from "~/api/queries.ts";
import type { Derp } from "~/api/queries.ts";
import type { SetDerpRequestBody } from "~/api/schema.gen.ts";
import { ifMatchInit, isStaleSettings } from "~/components/derp/model.ts";
import { toast } from "~/components/ui/toast.ts";

/** What a change to the relay settings sends: the whole configuration. */
export interface SetDerpVariables {
  readonly body: SetDerpRequestBody;
}

/**
 * The request that replaces the settings. It is not `api.useMutation`, because every change also
 * has to carry the ETag of the read it was made from, which the callers must not have to think
 * about.
 */
export type SetDerp = UseMutationResult<Derp, Error, SetDerpVariables>;

export interface DerpMutations {
  /** Replaces the whole configuration; every editor sends the full settings. */
  readonly set: SetDerp;
  readonly reset: Mutation<"delete", "/api/v1/derp">;
  readonly refresh: Mutation<"post", "/api/v1/derp/refresh">;
  /** Sends `next` and toasts `message` on success; the inline controls use it. */
  readonly apply: (next: SetDerpRequestBody, message: string) => void;
  /** Whether the last change was refused because the settings had moved on. */
  readonly stale: boolean;
  /** Reads the settings again and clears {@link stale}. */
  readonly reload: () => void;
}

export function useDerpMutations(): DerpMutations {
  const queryClient = useQueryClient();
  const [stale, setStale] = useState(false);
  const refresh = async (): Promise<void> => {
    await invalidate(queryClient, "/api/v1/derp", "/api/v1/server");
  };
  const set = useMutation({
    mutationFn: async ({ body }: SetDerpVariables): Promise<Derp> => {
      const etag = queryClient.getQueryData(derpQuery.queryKey)?.etag ?? "";
      const { data, response } = await fetchClient.PUT("/api/v1/derp", {
        body,
        ...ifMatchInit(etag),
      });

      if (data === undefined) {
        throw new ApiError(response.status, undefined, "The server sent no relay settings.");
      }

      return data;
    },
    onSuccess: async () => {
      setStale(false);
      await refresh();
    },
    onError: (error) => {
      setStale(isStaleSettings(error));
    },
  });
  const reset = api.useMutation("delete", "/api/v1/derp", {
    onSuccess: async () => {
      toast.success("Relay settings reset to the config file");
      await refresh();
    },
  });
  const refetch = api.useMutation("post", "/api/v1/derp/refresh", {
    onSuccess: async () => {
      toast.success("Relay maps refetched");
      await refresh();
    },
    onError: (error) => {
      toast.error("Could not refetch the relay maps", error);
    },
  });

  return {
    set,
    reset,
    refresh: refetch,
    stale,
    reload: () => {
      setStale(false);
      void refresh();
    },
    apply: (next, message) => {
      set.mutate(
        { body: next },
        {
          onSuccess: () => {
            toast.success(message);
          },
          onError: (error) => {
            // A stale ETag is answered by the callout on the page, which offers the way out.
            if (!isStaleSettings(error)) {
              toast.error("Could not change relays", error);
            }
          },
        },
      );
    },
  };
}
