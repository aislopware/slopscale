import { queryOptions } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import type { MethodResponse } from "openapi-react-query";

import { api, fetchClient } from "~/api/client.ts";
import { ApiError } from "~/api/error.ts";

export type Node = MethodResponse<typeof api, "get", "/api/v1/node">["nodes"][number];
export type User = MethodResponse<typeof api, "get", "/api/v1/user">["users"][number];
export type PreAuthKey = MethodResponse<
  typeof api,
  "get",
  "/api/v1/preauthkey"
>["preAuthKeys"][number];
export type ApiKey = MethodResponse<typeof api, "get", "/api/v1/apikey">["apiKeys"][number];
export type Settings = MethodResponse<typeof api, "get", "/api/v1/settings">;
export type Policy = MethodResponse<typeof api, "get", "/api/v1/policy">;

export const nodesQuery = api.queryOptions("get", "/api/v1/node");
export const usersQuery = api.queryOptions("get", "/api/v1/user");
export const preAuthKeysQuery = api.queryOptions("get", "/api/v1/preauthkey");
export const apiKeysQuery = api.queryOptions("get", "/api/v1/apikey");
export const settingsQuery = api.queryOptions("get", "/api/v1/settings");
/** What the server reports before any policy has been stored. */
export const emptyPolicy: Policy = { policy: "", updatedAt: "" };

/** The stored policy, or {@link emptyPolicy} when the server answers 404 because none is set. */
export const policyQuery = queryOptions({
  queryKey: ["get", "/api/v1/policy"] as const,
  queryFn: async (): Promise<Policy> => {
    try {
      const { data } = await fetchClient.GET("/api/v1/policy");

      return data ?? emptyPolicy;
    } catch (error) {
      if (error instanceof ApiError && error.notFound) {
        return emptyPolicy;
      }

      throw error;
    }
  },
});

type Collection =
  | "/api/v1/node"
  | "/api/v1/user"
  | "/api/v1/preauthkey"
  | "/api/v1/apikey"
  | "/api/v1/settings"
  | "/api/v1/policy";

/** Refetches every query under the given paths; a node change touches the node list and its detail. */
export async function invalidate(
  queryClient: QueryClient,
  ...paths: readonly Collection[]
): Promise<void> {
  await Promise.all(
    paths.map((path) =>
      queryClient.invalidateQueries({
        predicate: (query) =>
          typeof query.queryKey[1] === "string" && query.queryKey[1].startsWith(path),
      }),
    ),
  );
}
