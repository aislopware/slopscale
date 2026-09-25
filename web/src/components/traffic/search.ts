import { fallback, optional, picklist, pipe, transform, unknown } from "valibot";

import { destinationGroupings, nameGroupings } from "~/api/traffic.ts";
import type {
  DestinationFilters,
  DestinationGrouping,
  NameFilters,
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

/** A flag from the address: the router reads `lan=true` as a boolean, a hand-typed one as text. */
function toFlag(value: unknown): boolean {
  return value === true || value === "true";
}

const countValue = pipe(unknown(), transform(toCount));
const optionalCount = fallback(optional(countValue, 0), 0);
const flagValue = pipe(unknown(), transform(toFlag));
const optionalFlag = fallback(optional(flagValue, false), false);
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
  /** What the operator typed: hosts or addresses containing it. */
  readonly q: string;
  /** A picked host, exactly. */
  readonly host: string;
  /** A picked address, exactly. */
  readonly dst: string;
  /** Only private destinations: the picked LAN row. */
  readonly lan: boolean;
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
  host: optionalText,
  dst: optionalText,
  lan: optionalFlag,
  asn: optionalCount,
  country: optionalText,
  proto: optionalCount,
  port: optionalCount,
  node: optionalText,
};

/** The DNS page's address. */
export interface NameSearch extends TrafficWindowSearch {
  readonly by: NameGrouping;
  /** What the operator typed: names containing it. */
  readonly q: string;
  /** A picked name, exactly. */
  readonly name: string;
  readonly node: string;
}

export const nameSearchEntries = {
  ...trafficWindowEntries,
  by: fallback(optional(nameGroupingValue, "name"), "name"),
  q: optionalText,
  name: optionalText,
  node: optionalText,
};

/** The narrowing of a destinations read, without the window. */
export function destinationFilters(search: DestinationSearch, limit: number): DestinationFilters {
  return {
    groupBy: search.by,
    q: search.q,
    host: search.host,
    dst: search.dst,
    lan: search.lan,
    asn: search.asn,
    country: search.country,
    proto: search.proto,
    port: search.port,
    limit,
  };
}

/** The narrowing of a names read, without the window. */
export function nameFilters(search: NameSearch, limit: number): NameFilters {
  return { groupBy: search.by, q: search.q, name: search.name, limit };
}

/** The part of a destinations search that narrows rows, all at "everything". */
export const noDestinationFilters = {
  q: "",
  host: "",
  dst: "",
  lan: false,
  asn: 0,
  country: "",
  proto: 0,
  port: 0,
  node: "",
} as const;

type Pick = (row: TrafficDestination) => Partial<DestinationSearch>;

/** A group of private destinations has no network or country of its own; it opens as the LAN. */
function lanOr(
  row: TrafficDestination,
  pick: Partial<DestinationSearch>,
): Partial<DestinationSearch> {
  return row.private ? { lan: true, by: "node" } : { ...pick, by: "node" };
}

const picks: Record<DestinationGrouping, Pick> = {
  host: (row) => ({ host: row.host, by: "node" }),
  destination: (row) => ({ dst: row.dst, proto: row.proto, port: row.port, by: "node" }),
  asn: (row) => lanOr(row, { asn: row.asn }),
  country: (row) => lanOr(row, { country: row.country }),
  port: (row) => ({ proto: row.proto, port: row.port, by: "node" }),
  reporter: (row) => ({ gateway: row.nodeId, by: "host" }),
  node: (row) => ({ node: row.nodeId, by: "host" }),
};

/**
 * What clicking a row narrows to: a place, as the machines that reached exactly it; a machine or
 * gateway, as the hosts reached through it.
 */
export function pickDestination(
  row: TrafficDestination,
  groupBy: DestinationGrouping,
): Partial<DestinationSearch> {
  return picks[groupBy](row);
}

/** A clicked name, as the machines that looked up exactly it; a clicked machine, as its names. */
export function pickName(row: TrafficName, groupBy: NameGrouping): Partial<NameSearch> {
  return groupBy === "node" ? { node: row.nodeId, by: "name" } : { name: row.name, by: "node" };
}

type ChipOf<Search> = (name: string, value: string, cleared: Partial<Search>) => FilterChip;

/** Chips for one search, each handing it back with the fields it stands for cleared on removal. */
function chipsFor<Search>(search: Search, onChange: (next: Search) => void): ChipOf<Search> {
  return (name, value, cleared) => ({
    name,
    value,
    onRemove: () => {
      onChange({ ...search, ...cleared });
    },
  });
}

/**
 * A chip for each narrowing in force that the search box does not already show, so a filter that
 * came from a click is spelled out and has a way back.
 */
export function destinationChips(
  search: DestinationSearch,
  machineName: (id: string) => string,
  onChange: (next: DestinationSearch) => void,
): FilterChip[] {
  const chips: FilterChip[] = [];
  const chip = chipsFor(search, onChange);

  if (search.host !== "") {
    chips.push(chip("Host", search.host, { host: "" }));
  }

  if (search.dst !== "") {
    chips.push(chip("Address", search.dst, { dst: "" }));
  }

  if (search.lan) {
    chips.push(chip("Network", "LAN", { lan: false }));
  }

  if (search.asn !== 0) {
    chips.push(chip("Network", networkLabel(search.asn, ""), { asn: 0 }));
  }

  if (search.country !== "") {
    chips.push(chip("Country", countryName(search.country), { country: "" }));
  }

  if (search.proto !== 0) {
    chips.push(chip("Port", portLabel(search.proto, search.port), { proto: 0, port: 0 }));
  }

  if (search.node !== "") {
    chips.push(chip("Machine", machineName(search.node), { node: "" }));
  }

  return chips;
}

/** The chips of a names search: a picked name, and a machine the picker cannot show. */
export function nameChips(
  search: NameSearch,
  machineName: ((id: string) => string) | undefined,
  onChange: (next: NameSearch) => void,
): FilterChip[] {
  const chips: FilterChip[] = [];
  const chip = chipsFor(search, onChange);

  if (search.name !== "") {
    chips.push(chip("Name", search.name, { name: "" }));
  }

  if (search.node !== "" && machineName !== undefined) {
    chips.push(chip("Machine", machineName(search.node), { node: "" }));
  }

  return chips;
}
