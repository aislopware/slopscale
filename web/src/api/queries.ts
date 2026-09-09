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
export type OAuthClient = MethodResponse<
  typeof api,
  "get",
  "/api/v1/oauth-client"
>["oauthClients"][number];
export type Settings = MethodResponse<typeof api, "get", "/api/v1/settings">;
export type TailnetLock = MethodResponse<typeof api, "get", "/api/v1/tailnet-lock">;
export type ServerInfo = MethodResponse<typeof api, "get", "/api/v1/server">;
export type Policy = MethodResponse<typeof api, "get", "/api/v1/policy">;
export type AuditPage = MethodResponse<typeof api, "get", "/api/v1/audit">;
export type AuditEvent = AuditPage["events"][number];
export type Group = MethodResponse<typeof api, "get", "/api/v1/group">["groups"][number];
export type AccessRule = MethodResponse<typeof api, "get", "/api/v1/access-rule">["rules"][number];
export type Posture = MethodResponse<typeof api, "get", "/api/v1/posture">["postures"][number];
export type AccessRequest = MethodResponse<
  typeof api,
  "get",
  "/api/v1/access-request"
>["requests"][number];
export type Dns = MethodResponse<typeof api, "get", "/api/v1/dns">;
export type Derp = MethodResponse<typeof api, "get", "/api/v1/derp">;
export type Network = MethodResponse<typeof api, "get", "/api/v1/network">["networks"][number];
export type DnsRule = MethodResponse<typeof api, "get", "/api/v1/dns/rule">["rules"][number];
export type Webhook = MethodResponse<typeof api, "get", "/api/v1/webhook">["webhooks"][number];
export type Service = MethodResponse<typeof api, "get", "/api/v1/services">["services"][number];
/** One machine's standing towards a service: what it announces and whether it may host it. */
export type ServiceHost = Service["hosts"][number];
export type App = MethodResponse<typeof api, "get", "/api/v1/apps">["apps"][number];
/** A connector node of an app: what it advertises and whether it runs the connector. */
export type AppNode = App["nodes"][number];

/**
 * How long the collections every page reads stay fresh. The sidebar badges and the command palette
 * read the machines and the users on top of whichever page is open, so without this each of them
 * refetched on every navigation. A mutation invalidates what it changed, so a stale minute is only
 * about someone else's change arriving late.
 */
export const sharedStaleTime = 60_000;

export const nodesQuery = api.queryOptions("get", "/api/v1/node", undefined, {
  staleTime: sharedStaleTime,
});
export const usersQuery = api.queryOptions("get", "/api/v1/user", undefined, {
  staleTime: sharedStaleTime,
});
export const preAuthKeysQuery = api.queryOptions("get", "/api/v1/preauthkey");
export const apiKeysQuery = api.queryOptions("get", "/api/v1/apikey");
export const oauthClientsQuery = api.queryOptions("get", "/api/v1/oauth-client");
export const settingsQuery = api.queryOptions("get", "/api/v1/settings");
export const tailnetLockQuery = api.queryOptions("get", "/api/v1/tailnet-lock");
export const serverInfoQuery = api.queryOptions("get", "/api/v1/server");
export const groupsQuery = api.queryOptions("get", "/api/v1/group", undefined, {
  staleTime: sharedStaleTime,
});
export const accessRulesQuery = api.queryOptions("get", "/api/v1/access-rule");
export const posturesQuery = api.queryOptions("get", "/api/v1/posture");
export const accessRequestsQuery = api.queryOptions("get", "/api/v1/access-request");
export const myAccessRequestsQuery = api.queryOptions("get", "/api/v1/access-request", {
  params: { query: { mine: true } },
});
export const accessRequestOptionsQuery = api.queryOptions("get", "/api/v1/access-request/options");
export type AccessRequestOptions = MethodResponse<
  typeof api,
  "get",
  "/api/v1/access-request/options"
>;

/** The whole graph, which is also the shape of one machine's: the machines never narrow. */
const wholeAccessGraphQuery = api.queryOptions("get", "/api/v1/access-graph", {
  params: { query: {} },
});

export type AccessGraphQuery = typeof wholeAccessGraphQuery;

/**
 * Who reaches what, read off the rules the server hands each machine. With a machine id the server
 * keeps only the edges that machine is an end of; the machines it lists stay the whole tailnet, so
 * the picker works either way.
 */
export function accessGraphQuery(node: string): AccessGraphQuery {
  return api.queryOptions("get", "/api/v1/access-graph", {
    params: { query: node === "" ? {} : { node } },
  });
}
export const dnsQuery = api.queryOptions("get", "/api/v1/dns");
export const dnsRulesQuery = api.queryOptions("get", "/api/v1/dns/rule");
export const networksQuery = api.queryOptions("get", "/api/v1/network");
export const servicesQuery = api.queryOptions("get", "/api/v1/services");
export const appsQuery = api.queryOptions("get", "/api/v1/apps");
export const webhooksQuery = api.queryOptions("get", "/api/v1/webhook");

export type LogStream = MethodResponse<
  typeof api,
  "get",
  "/api/v1/log-stream"
>["logStreams"][number];

export const logStreamsQuery = api.queryOptions("get", "/api/v1/log-stream");
export const webhookEventTypesQuery = api.queryOptions("get", "/api/v1/webhook/event-types");

export type PostureIntegration = MethodResponse<
  typeof api,
  "get",
  "/api/v1/posture-integrations"
>["integrations"][number];

export type PostureProvider = MethodResponse<
  typeof api,
  "get",
  "/api/v1/posture-integrations/providers"
>["providers"][number];

export const postureIntegrationsQuery = api.queryOptions("get", "/api/v1/posture-integrations");
export const postureProvidersQuery = api.queryOptions(
  "get",
  "/api/v1/posture-integrations/providers",
);

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

/** The relay settings as they were read, with the ETag identifying that read. */
export interface DerpSnapshot {
  readonly derp: Derp;
  /** What a change sends back as If-Match; "" when the server answered without an ETag. */
  readonly etag: string;
}

/**
 * The relay settings and the ETag of the read. `openapi-react-query` keeps the body only, and the
 * ETag is what a later PUT sends as If-Match to be refused when someone else changed the settings
 * in between, so the read goes through the raw client.
 */
export const derpQuery = queryOptions({
  queryKey: ["get", "/api/v1/derp"] as const,
  queryFn: async (): Promise<DerpSnapshot> => {
    const { data, response } = await fetchClient.GET("/api/v1/derp");

    if (data === undefined) {
      throw new ApiError(response.status, undefined, "The server sent no relay settings.");
    }

    return { derp: data, etag: response.headers.get("ETag") ?? "" };
  },
});

/**
 * How often the relay latency page asks again. Clients send a network report as they move between
 * networks, so the page is live; only while the tab is in front, like the machine list.
 */
export const derpLatencyPollMs = 60_000;

/** What every machine last measured to each relay region, worst machines first. */
export const derpLatencyQuery = api.queryOptions("get", "/api/v1/derp/latency", undefined, {
  refetchInterval: derpLatencyPollMs,
  refetchIntervalInBackground: false,
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

export type SSHRecordingPage = MethodResponse<typeof api, "get", "/api/v1/ssh-recording">;
export type SSHRecording = SSHRecordingPage["recordings"][number];

/** Page size for recorded sessions; the server allows up to 500. */
export const sshRecordingPageSize = 50;

const emptySSHRecordingPage: SSHRecordingPage = { recordings: [], nextBefore: "" };

type SSHRecordingQueryKey = readonly ["get", "/api/v1/ssh-recording"];

/** Recorded SSH sessions, newest first, paged with `before` like the audit log. */
export const sshRecordingsQuery: UnusedSkipTokenInfiniteOptions<
  SSHRecordingPage,
  Error,
  InfiniteData<SSHRecordingPage>,
  SSHRecordingQueryKey,
  string
> = infiniteQueryOptions({
  queryKey: ["get", "/api/v1/ssh-recording"] as const,
  queryFn: async ({ pageParam }): Promise<SSHRecordingPage> => {
    const { data } = await fetchClient.GET("/api/v1/ssh-recording", {
      params: {
        query: {
          limit: sshRecordingPageSize,
          ...(pageParam === "" ? {} : { before: pageParam }),
        },
      },
    });

    return data ?? emptySSHRecordingPage;
  },
  initialPageParam: "",
  getNextPageParam: (page) => (page.nextBefore === "" ? undefined : page.nextBefore),
});

/** Where the browser fetches a recording's asciinema file; the session cookie authorises it. */
export function sshRecordingCastUrl(id: string): string {
  return `/api/v1/ssh-recording/${encodeURIComponent(id)}/cast`;
}

/**
 * The queries below ask the machine itself rather than the server's records: each one is a round
 * trip over the client's control connection, which only a connected machine answers. So they are
 * keyed by node id, kept for the shared stale minute, only ever run from that machine's own page,
 * and every caller gates them on `node.online`.
 *
 * Each is written as a factory over a prototype: `api.queryOptions` builds its option type out of
 * the path, and naming that type any other way would mean spelling out the whole generic.
 */
const liveNodeOptions = { staleTime: sharedStaleTime };
const anyNode = { params: { path: { nodeId: "" } } };

const healthPrototype = api.queryOptions(
  "get",
  "/api/v1/node/{nodeId}/health",
  anyNode,
  liveNodeOptions,
);

/** The warnings the client would show its own user: where a connected but broken machine says so. */
export function nodeHealthQuery(nodeId: string): typeof healthPrototype {
  return api.queryOptions(
    "get",
    "/api/v1/node/{nodeId}/health",
    { params: { path: { nodeId } } },
    liveNodeOptions,
  );
}

const preferencesPrototype = api.queryOptions(
  "get",
  "/api/v1/node/{nodeId}/preferences",
  anyNode,
  liveNodeOptions,
);

/** The curated preferences the machine's owner set; changing them needs remote configuration on. */
export function nodePreferencesQuery(nodeId: string): typeof preferencesPrototype {
  return api.queryOptions(
    "get",
    "/api/v1/node/{nodeId}/preferences",
    { params: { path: { nodeId } } },
    liveNodeOptions,
  );
}

const tlsCertPrototype = api.queryOptions(
  "get",
  "/api/v1/node/{nodeId}/tls-cert",
  anyNode,
  liveNodeOptions,
);

/** The certificate the machine caches for its own name, which Serve and Funnel need. */
export function nodeTlsCertQuery(nodeId: string): typeof tlsCertPrototype {
  return api.queryOptions(
    "get",
    "/api/v1/node/{nodeId}/tls-cert",
    { params: { path: { nodeId } } },
    liveNodeOptions,
  );
}

const appConnectorRoutesPrototype = api.queryOptions(
  "get",
  "/api/v1/node/{nodeId}/app-connector-routes",
  anyNode,
  liveNodeOptions,
);

/** The addresses an app connector has resolved for the domains it answers for. */
export function nodeAppConnectorRoutesQuery(nodeId: string): typeof appConnectorRoutesPrototype {
  return api.queryOptions(
    "get",
    "/api/v1/node/{nodeId}/app-connector-routes",
    { params: { path: { nodeId } } },
    liveNodeOptions,
  );
}

const sshUsernamesPrototype = api.queryOptions(
  "get",
  "/api/v1/node/{nodeId}/ssh-usernames",
  anyNode,
  liveNodeOptions,
);

/**
 * The logins the machine would suggest for a Tailscale SSH session. They are a hint, not an
 * authorisation: the SSH policy still decides, so the terminal offers them and ignores a refusal.
 */
export function nodeSshUsernamesQuery(nodeId: string): typeof sshUsernamesPrototype {
  return api.queryOptions(
    "get",
    "/api/v1/node/{nodeId}/ssh-usernames",
    { params: { path: { nodeId } } },
    liveNodeOptions,
  );
}

/** The dumps a client hands over for support, in the order the console offers them. */
export const nodeDiagnosticKinds = [
  "prefs",
  "netmap",
  "metrics",
  "goroutines",
  "sockstats",
  "tka-log",
] as const;

export type NodeDiagnosticKind = (typeof nodeDiagnosticKinds)[number];

/**
 * Where the browser fetches one diagnostic. It is a download rather than a query: the body is the
 * client's own file, which the server passes through with the name it should be saved under.
 */
export function nodeDiagnosticUrl(nodeId: string, kind: NodeDiagnosticKind): string {
  return `/api/v1/node/${encodeURIComponent(nodeId)}/diagnostics/${kind}`;
}

type Collection =
  | "/api/v1/node"
  | "/api/v1/user"
  | "/api/v1/preauthkey"
  | "/api/v1/apikey"
  | "/api/v1/oauth-client"
  | "/api/v1/settings"
  | "/api/v1/tailnet-lock"
  | "/api/v1/policy"
  | "/api/v1/group"
  | "/api/v1/access-rule"
  | "/api/v1/posture"
  | "/api/v1/access-request"
  | "/api/v1/access-graph"
  | "/api/v1/dns"
  | "/api/v1/dns/rule"
  | "/api/v1/derp"
  | "/api/v1/server"
  | "/api/v1/network"
  | "/api/v1/services"
  | "/api/v1/apps"
  | "/api/v1/webhook"
  | "/api/v1/log-stream"
  | "/api/v1/posture-integration"
  | "/api/v1/posture-integrations"
  | "/api/v1/ssh-recording"
  | "/api/v1/auth/sessions"
  | "/api/v1/invite";

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

export type ConsoleSession = MethodResponse<
  typeof api,
  "get",
  "/api/v1/auth/sessions"
>["sessions"][number];

/**
 * The console sign-ins that have not expired. The server decides the scope: a caller who may manage
 * users gets every session, anyone else only their own.
 */
export const sessionsQuery = api.queryOptions("get", "/api/v1/auth/sessions");

/** The file formats the audit export offers; the query parameter takes them verbatim. */
export const auditExportFormats = ["csv", "json"] as const;

export type AuditExportFormat = (typeof auditExportFormats)[number];

/**
 * Where the browser fetches the events matching `filters` as a file. The same filters the list
 * sends, so an export holds exactly the rows on screen; the session cookie authorises it, the way
 * it authorises a recording's cast file.
 */
export function auditExportUrl(filters: AuditFilters, format: AuditExportFormat): string {
  const query = new URLSearchParams({ format });

  if (filters.action !== "") {
    query.set("action", filters.action);
  }

  if (filters.actorUserId !== "") {
    query.set("actorUserId", filters.actorUserId);
  }

  if (filters.range !== "all") {
    query.set("since", hoursAgo(rangeHours[filters.range]));
  }

  return `/api/v1/audit/export?${query.toString()}`;
}

export type Invite = MethodResponse<typeof api, "get", "/api/v1/invite">["invites"][number];

/**
 * Every invitation, pending and accepted; the users page filters the accepted ones out. It sits
 * under the users table, so it keeps that table's stale window rather than asking again on every
 * visit; sending, re-sending and revoking one invalidate it.
 */
export const invitesQuery = api.queryOptions("get", "/api/v1/invite", undefined, {
  staleTime: sharedStaleTime,
});
