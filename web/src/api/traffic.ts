import { queryOptions } from "@tanstack/react-query";
import type { UseQueryOptions } from "@tanstack/react-query";
import type { MethodResponse } from "openapi-react-query";

import { api, fetchClient } from "~/api/client.ts";
import { isLive, trafficWindow } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";

export type TrafficSummary = MethodResponse<typeof api, "get", "/api/v1/traffic/summary">;
export type TrafficPoint = TrafficSummary["series"][number];
export type TrafficNode = TrafficSummary["nodes"][number];
export type TrafficDestinations = MethodResponse<typeof api, "get", "/api/v1/traffic/destinations">;
export type TrafficDestination = TrafficDestinations["destinations"][number];
export type TrafficNames = MethodResponse<typeof api, "get", "/api/v1/traffic/dns">;
export type TrafficName = TrafficNames["names"][number];
export type TrafficReporters = MethodResponse<typeof api, "get", "/api/v1/traffic/reporters">;
export type TrafficReporter = TrafficReporters["reporters"][number];
export type TrafficSettings = MethodResponse<typeof api, "get", "/api/v1/traffic/settings">;

/** How a destinations read groups its rows; "node" and "reporter" list machines and gateways. */
export const destinationGroupings = [
  "host",
  "destination",
  "asn",
  "country",
  "port",
  "node",
  "reporter",
] as const;

export type DestinationGrouping = (typeof destinationGroupings)[number];

export const nameGroupings = ["name", "node"] as const;

export type NameGrouping = (typeof nameGroupings)[number];

/**
 * A window that moves with the clock is read again every minute, the width of the finest bucket, so
 * a page left open follows the traffic. A custom window is history and stays as it was read.
 */
const liveRefetchMs = 60_000;

function refetchEvery(window: TrafficWindowSearch): number | false {
  return isLive(window) ? liveRefetchMs : false;
}

/** The window, gateway and machine a read is narrowed to; the ids are "" for all of them. */
export interface TrafficScope extends TrafficWindowSearch {
  readonly node: string;
}

function rangeQuery(scope: TrafficScope): {
  start: string;
  end: string;
  nodeId?: string;
  reporterId?: string;
} {
  return {
    ...trafficWindow(scope),
    ...(scope.node === "" ? {} : { nodeId: scope.node }),
    ...(scope.gateway === "" ? {} : { reporterId: scope.gateway }),
  };
}

const emptyCounts = { conns: 0, rxBytes: 0, rxPackets: 0, txBytes: 0, txPackets: 0 };

const emptySummary: TrafficSummary = {
  start: "",
  end: "",
  resolution: 0,
  total: emptyCounts,
  series: [],
  nodes: [],
  reporters: [],
};

type SummaryKey = readonly ["get", "/api/v1/traffic/summary", TrafficScope, number];

/**
 * Totals, the series and the busiest machines and gateways over the window. The key holds the
 * search, not the computed bounds, and the bounds are worked out when the read runs, so the loader
 * and the page ask for the same entry and a refetch reads up to the minute it runs in.
 */
export function trafficSummaryQuery(
  scope: TrafficScope,
  limit: number,
): UseQueryOptions<TrafficSummary, Error, TrafficSummary, SummaryKey> &
  Required<Pick<UseQueryOptions<TrafficSummary, Error, TrafficSummary, SummaryKey>, "queryKey">> {
  return queryOptions({
    queryKey: ["get", "/api/v1/traffic/summary", scope, limit] as const,
    queryFn: async (): Promise<TrafficSummary> => {
      const { data } = await fetchClient.GET("/api/v1/traffic/summary", {
        params: { query: { ...rangeQuery(scope), limit } },
      });

      return data ?? emptySummary;
    },
    refetchInterval: refetchEvery(scope),
  });
}

/** What narrows a destinations read beyond the window: the values of a row clicked through. */
export interface DestinationFilters {
  readonly groupBy: DestinationGrouping;
  /** Hosts or addresses containing this. */
  readonly q: string;
  readonly asn: number;
  readonly country: string;
  readonly proto: number;
  readonly port: number;
  readonly limit: number;
}

type DestinationsKey = readonly [
  "get",
  "/api/v1/traffic/destinations",
  TrafficScope,
  DestinationFilters,
];

const emptyDestinations: TrafficDestinations = {
  start: "",
  end: "",
  resolution: 0,
  destinations: [],
};

export function trafficDestinationsQuery(
  scope: TrafficScope,
  filters: DestinationFilters,
): UseQueryOptions<TrafficDestinations, Error, TrafficDestinations, DestinationsKey> &
  Required<
    Pick<
      UseQueryOptions<TrafficDestinations, Error, TrafficDestinations, DestinationsKey>,
      "queryKey"
    >
  > {
  return queryOptions({
    queryKey: ["get", "/api/v1/traffic/destinations", scope, filters] as const,
    queryFn: async (): Promise<TrafficDestinations> => {
      const { data } = await fetchClient.GET("/api/v1/traffic/destinations", {
        params: {
          query: {
            ...rangeQuery(scope),
            groupBy: filters.groupBy,
            limit: filters.limit,
            ...(filters.q === "" ? {} : { q: filters.q }),
            ...(filters.asn === 0 ? {} : { asn: filters.asn }),
            ...(filters.country === "" ? {} : { country: filters.country }),
            ...(filters.proto === 0 ? {} : { proto: filters.proto }),
            ...(filters.proto === 0 || filters.port === 0 ? {} : { port: filters.port }),
          },
        },
      });

      return data ?? emptyDestinations;
    },
    refetchInterval: refetchEvery(scope),
  });
}

export interface NameFilters {
  readonly groupBy: NameGrouping;
  readonly q: string;
  readonly limit: number;
}

type NamesKey = readonly ["get", "/api/v1/traffic/dns", TrafficScope, NameFilters];

const emptyNames: TrafficNames = { start: "", end: "", resolution: 0, names: [] };

export function trafficNamesQuery(
  scope: TrafficScope,
  filters: NameFilters,
): UseQueryOptions<TrafficNames, Error, TrafficNames, NamesKey> &
  Required<Pick<UseQueryOptions<TrafficNames, Error, TrafficNames, NamesKey>, "queryKey">> {
  return queryOptions({
    queryKey: ["get", "/api/v1/traffic/dns", scope, filters] as const,
    queryFn: async (): Promise<TrafficNames> => {
      const { data } = await fetchClient.GET("/api/v1/traffic/dns", {
        params: {
          query: {
            ...rangeQuery(scope),
            groupBy: filters.groupBy,
            limit: filters.limit,
            ...(filters.q === "" ? {} : { q: filters.q }),
          },
        },
      });

      return data ?? emptyNames;
    },
    refetchInterval: refetchEvery(scope),
  });
}

/** A gateway reports every minute, so its state is worth reading as often. */
export const trafficReportersQuery = api.queryOptions(
  "get",
  "/api/v1/traffic/reporters",
  undefined,
  { refetchInterval: liveRefetchMs },
);

export const trafficSettingsQuery = api.queryOptions("get", "/api/v1/traffic/settings");
