import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import type {
  InfiniteData,
  QueryClient,
  UnusedSkipTokenInfiniteOptions,
} from "@tanstack/react-query";
import type { MethodResponse } from "openapi-react-query";

import { api, fetchClient } from "~/api/client.ts";
import { ApiError } from "~/api/error.ts";
import { hoursAgo } from "~/lib/time.ts";

export type Node = MethodResponse<typeof api, "get", "/api/v1/node">["nodes"][number];
export type User = MethodResponse<typeof api, "get", "/api/v1/user">["users"][number];
export type PreAuthKey = MethodResponse<
  typeof api,
  "get",
  "/api/v1/preauthkey"
>["preAuthKeys"][number];
export type ApiKey = MethodResponse<typeof api, "get", "/api/v1/apikey">["apiKeys"][number];
export type Settings = MethodResponse<typeof api, "get", "/api/v1/settings">;
export type ServerInfo = MethodResponse<typeof api, "get", "/api/v1/server">;
export type Policy = MethodResponse<typeof api, "get", "/api/v1/policy">;
export type AuditPage = MethodResponse<typeof api, "get", "/api/v1/audit">;
export type AuditEvent = AuditPage["events"][number];
export type Group = MethodResponse<typeof api, "get", "/api/v1/group">["groups"][number];
export type AccessRule = MethodResponse<typeof api, "get", "/api/v1/access-rule">["rules"][number];
export type Posture = MethodResponse<typeof api, "get", "/api/v1/posture">["postures"][number];
export type Dns = MethodResponse<typeof api, "get", "/api/v1/dns">;
export type Network = MethodResponse<typeof api, "get", "/api/v1/network">["networks"][number];
export type Webhook = MethodResponse<typeof api, "get", "/api/v1/webhook">["webhooks"][number];

export const nodesQuery = api.queryOptions("get", "/api/v1/node");
export const usersQuery = api.queryOptions("get", "/api/v1/user");
export const preAuthKeysQuery = api.queryOptions("get", "/api/v1/preauthkey");
export const apiKeysQuery = api.queryOptions("get", "/api/v1/apikey");
export const settingsQuery = api.queryOptions("get", "/api/v1/settings");
export const serverInfoQuery = api.queryOptions("get", "/api/v1/server");
export const groupsQuery = api.queryOptions("get", "/api/v1/group");
export const accessRulesQuery = api.queryOptions("get", "/api/v1/access-rule");
export const posturesQuery = api.queryOptions("get", "/api/v1/posture");
export const dnsQuery = api.queryOptions("get", "/api/v1/dns");
export const networksQuery = api.queryOptions("get", "/api/v1/network");
export const webhooksQuery = api.queryOptions("get", "/api/v1/webhook");
export const webhookEventTypesQuery = api.queryOptions("get", "/api/v1/webhook/event-types");

export type WebhookDelivery = MethodResponse<
  typeof api,
  "get",
  "/api/v1/webhook/{id}/deliveries"
>["deliveries"][number];

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

/** The time ranges the audit page offers; the preset, not an instant, keys the query. */
export const auditRanges = ["1h", "24h", "7d", "30d", "all"] as const;

export type AuditRange = (typeof auditRanges)[number];

/** How far back each preset reaches, in hours; "all" sends no lower bound. */
const rangeHours: Record<Exclude<AuditRange, "all">, number> = {
  "1h": 1,
  "24h": 24,
  "7d": 168,
  "30d": 720,
};

export interface AuditFilters {
  /** One action (`node.delete`) or a prefix ending in a dot (`node.`); "" for every action. */
  readonly action: string;
  /** Keep events by this user id; "" for every actor. */
  readonly actorUserId: string;
  readonly range: AuditRange;
}

/** Page size: the server allows up to 500, and a screenful of audit rows is far less. */
export const auditPageSize = 100;

const emptyAuditPage: AuditPage = { events: [], nextBefore: "" };

type AuditQueryKey = readonly ["get", "/api/v1/audit", AuditFilters];

/**
 * One page of audit events per fetch, newest first, paged with `before` (the last id of the page
 * before). The filters key the query, so changing one starts its own list; the range is resolved to
 * a timestamp only when the request goes out.
 */
export function auditQuery(
  filters: AuditFilters,
): UnusedSkipTokenInfiniteOptions<
  AuditPage,
  Error,
  InfiniteData<AuditPage>,
  AuditQueryKey,
  string
> {
  return infiniteQueryOptions({
    queryKey: ["get", "/api/v1/audit", filters] as const,
    queryFn: async ({ pageParam }): Promise<AuditPage> => {
      const { data } = await fetchClient.GET("/api/v1/audit", {
        params: {
          query: {
            limit: auditPageSize,
            ...(filters.action === "" ? {} : { action: filters.action }),
            ...(filters.actorUserId === "" ? {} : { actorUserId: filters.actorUserId }),
            ...(filters.range === "all" ? {} : { since: hoursAgo(rangeHours[filters.range]) }),
            ...(pageParam === "" ? {} : { before: pageParam }),
          },
        },
      });

      return data ?? emptyAuditPage;
    },
    initialPageParam: "",
    getNextPageParam: (page) => (page.nextBefore === "" ? undefined : page.nextBefore),
  });
}

type Collection =
  | "/api/v1/node"
  | "/api/v1/user"
  | "/api/v1/preauthkey"
  | "/api/v1/apikey"
  | "/api/v1/settings"
  | "/api/v1/policy"
  | "/api/v1/group"
  | "/api/v1/access-rule"
  | "/api/v1/posture"
  | "/api/v1/dns"
  | "/api/v1/network"
  | "/api/v1/webhook";

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
