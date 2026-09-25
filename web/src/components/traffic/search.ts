import { fallback, optional, picklist, pipe, transform, unknown } from "valibot";

import { destinationGroupings, nameGroupings } from "~/api/traffic.ts";
import type {
  DestinationFilters,
  DestinationGrouping,
  NameGrouping,
  TrafficDestination,
  TrafficName,
} from "~/api/traffic.ts";
import type { FilterChip } from "~/components/machines/filter-chips.tsx";
import { countryName, networkLabel, portLabel } from "~/components/traffic/format.ts";
import { optionalText, trafficWindowEntries } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";

/** A whole number from the address, or 0 when it holds none. */
function toCount(value: unknown): number {
  const number = typeof value === "string" ? Number(value) : value;

  return typeof number === "number" && Number.isInteger(number) && number > 0 ? number : 0;
}

const countValue = pipe(unknown(), transform(toCount));
const optionalCount = fallback(optional(countValue, 0), 0);
const destinationGroupingValue = picklist(destinationGroupings);
const nameGroupingValue = picklist(nameGroupings);

/** The groupings a destinations page offers; machines and gateways are the "who" views. */
export const destinationTabs: readonly { value: DestinationGrouping; label: string }[] = [
  { value: "host", label: "Hosts" },
  { value: "destination", label: "Addresses" },
  { value: "asn", label: "Networks" },
  { value: "country", label: "Countries" },
  { value: "port", label: "Ports" },
  { value: "node", label: "Machines" },
];

function isDestinationGrouping(value: string): value is DestinationGrouping {
  return (destinationGroupings as readonly string[]).includes(value);
}

export function toDestinationGrouping(value: string): DestinationGrouping {
  return isDestinationGrouping(value) ? value : "host";
}

export function toNameGrouping(value: string): NameGrouping {
  return value === "node" ? "node" : "name";
}

/** The destinations page's address: the window, how rows group, and what narrows them. */
export interface DestinationSearch extends TrafficWindowSearch {
  readonly by: DestinationGrouping;
  readonly q: string;
  readonly asn: number;
  readonly country: string;
  readonly proto: number;
  readonly port: number;
  /** A machine to keep, or "" for all of them. */
  readonly node: string;
}

export const destinationSearchEntries = {
  ...trafficWindowEntries,
  by: fallback(optional(destinationGroupingValue, "host"), "host"),
  q: optionalText,
  asn: optionalCount,
  country: optionalText,
  proto: optionalCount,
  port: optionalCount,
  node: optionalText,
};

/** The DNS page's address. */
export interface NameSearch extends TrafficWindowSearch {
  readonly by: NameGrouping;
  readonly q: string;
  readonly node: string;
}

export const nameSearchEntries = {
  ...trafficWindowEntries,
  by: fallback(optional(nameGroupingValue, "name"), "name"),
  q: optionalText,
  node: optionalText,
};

/** The narrowing of a destinations read, without the window. */
export function destinationFilters(search: DestinationSearch, limit: number): DestinationFilters {
  return {
    groupBy: search.by,
    q: search.q,
    asn: search.asn,
    country: search.country,
    proto: search.proto,
    port: search.port,
    limit,
  };
}

/** The part of a destinations search that narrows rows, all at "everything". */
export const noDestinationFilters = {
  q: "",
  asn: 0,
  country: "",
  proto: 0,
  port: 0,
  node: "",
} as const;

type Pick = (row: TrafficDestination) => Partial<DestinationSearch>;

const picks: Record<DestinationGrouping, Pick> = {
  host: (row) => ({ q: row.host, by: "node" }),
  destination: (row) => ({ q: row.dst, by: "node" }),
  asn: (row) => ({ asn: row.asn, by: "node" }),
  country: (row) => ({ country: row.country, by: "node" }),
  port: (row) => ({ proto: row.proto, port: row.port, by: "node" }),
  reporter: (row) => ({ gateway: row.nodeId, by: "host" }),
  node: (row) => ({ node: row.nodeId, by: "host" }),
};

/**
 * What clicking a row narrows to: a place, as the machines that reached it; a machine or gateway,
 * as the hosts reached through it.
 */
export function pickDestination(
  row: TrafficDestination,
  groupBy: DestinationGrouping,
): Partial<DestinationSearch> {
  return picks[groupBy](row);
}

/** A clicked name, as the machines that looked it up; a clicked machine, as its names. */
export function pickName(row: TrafficName, groupBy: NameGrouping): Partial<NameSearch> {
  return groupBy === "node" ? { node: row.nodeId, by: "name" } : { q: row.name, by: "node" };
}

/**
 * A chip for each narrowing in force, so a filter that came from a click is spelled out and has a
 * way back. Each chip hands back the search with its own field cleared.
 */
export function destinationChips(
  search: DestinationSearch,
  machineName: (id: string) => string,
  onChange: (next: DestinationSearch) => void,
): FilterChip[] {
  const chips: FilterChip[] = [];

  if (search.q !== "") {
    chips.push({
      name: "Matching",
      value: search.q,
      onRemove: () => {
        onChange({ ...search, q: "" });
      },
    });
  }

  if (search.asn !== 0) {
    chips.push({
      name: "Network",
      value: networkLabel(search.asn, ""),
      onRemove: () => {
        onChange({ ...search, asn: 0 });
      },
    });
  }

  if (search.country !== "") {
    chips.push({
      name: "Country",
      value: countryName(search.country),
      onRemove: () => {
        onChange({ ...search, country: "" });
      },
    });
  }

  if (search.proto !== 0) {
    chips.push({
      name: "Port",
      value: portLabel(search.proto, search.port),
      onRemove: () => {
        onChange({ ...search, proto: 0, port: 0 });
      },
    });
  }

  if (search.node !== "") {
    chips.push({
      name: "Machine",
      value: machineName(search.node),
      onRemove: () => {
        onChange({ ...search, node: "" });
      },
    });
  }

  return chips;
}
