import type { Derp } from "~/api/queries.ts";
import type {
  DerpCustomRegion,
  DerpRelay,
  DerpServerSettings,
  DerpSettings,
  SetDerpRequestBody,
} from "~/api/schema.gen.ts";
import { isIpv4, isIpv6 } from "~/components/dns/model.ts";

/** Where a region of the map came from, as the server reports it. */
export type RegionSource = Derp["regions"][number]["source"];

export const sourceLabels: Record<RegionSource, string> = {
  tailscale: "Tailscale",
  url: "Map URL",
  file: "Map file",
  custom: "Your relay",
  embedded: "Embedded",
  config: "Config",
};

/** The Tailscale public map, the config file's default source. */
export const tailscaleMapUrl = "https://controlplane.tailscale.com/derpmap/default";

/** The refetch intervals offered; the server refuses anything under a minute. */
export const frequencyPresets: readonly string[] = ["15m", "1h", "3h", "6h", "12h", "24h"];

const minute = 60;
const minutesPerHour = 60;
const hour = minutesPerHour * minute;
const hoursPerDay = 24;
const day = hoursPerDay * hour;

const units: readonly (readonly [suffix: string, seconds: number])[] = [
  ["d", day],
  ["h", hour],
  ["m", minute],
  ["s", 1],
];

/** The largest unit that divides the duration evenly, with the count in it. */
function splitDuration(seconds: number): { readonly amount: number; readonly unit: string } {
  const fit = units.find(([, size]) => seconds % size === 0) ?? ["s", 1];

  return { amount: seconds / fit[1], unit: fit[0] };
}

/** Turns a Go duration such as "3h0m0s" into the short form the presets use. */
export function shortDuration(value: string): string {
  const seconds = durationSeconds(value);

  if (seconds === null || seconds <= 0) {
    return value;
  }

  const { amount, unit } = splitDuration(seconds);

  return `${amount}${unit}`;
}

const unitSeconds = new Map(units);
const unitNames = new Map([
  ["d", "day"],
  ["h", "hour"],
  ["m", "minute"],
  ["s", "second"],
]);
const durationPart = /(?<amount>\d+(?:\.\d+)?)(?<unit>[smhd])/gv;
const durationShape = /^(?:\d+(?:\.\d+)?[smhd])+$/v;

/** Seconds in a duration written like Go's, or null when it is not one. */
export function durationSeconds(value: string): number | null {
  const text = value.trim();

  if (!durationShape.test(text)) {
    return null;
  }

  let total = 0;

  for (const match of text.matchAll(durationPart)) {
    total +=
      Number(match.groups?.["amount"]) * (unitSeconds.get(match.groups?.["unit"] ?? "s") ?? 1);
  }

  return total;
}

export function frequencyLabel(value: string): string {
  const seconds = durationSeconds(value);

  if (seconds === null || seconds <= 0) {
    return `Every ${value}`;
  }

  const { amount, unit } = splitDuration(seconds);
  const name = unitNames.get(unit) ?? "second";

  return amount === 1 ? `Every ${name}` : `Every ${amount} ${name}s`;
}

/** A copy every editor starts from, so a PUT always carries the whole configuration. */
export function cloneSettings(settings: DerpSettings): SetDerpRequestBody {
  return {
    urls: [...settings.urls],
    regions: settings.regions.map((region) => cloneRegion(region)),
    autoUpdate: settings.autoUpdate,
    updateFrequency: settings.updateFrequency,
    server: { ...settings.server },
  };
}

function cloneRegion(region: DerpCustomRegion): DerpCustomRegion {
  const copy = structuredClone(region);

  copy.nodes ??= [];

  return copy;
}

export function withUrl(settings: DerpSettings, url: string): SetDerpRequestBody {
  const next = cloneSettings(settings);
  const urls = next.urls ?? [];

  if (!urls.includes(url)) {
    urls.push(url);
  }

  next.urls = urls;

  return next;
}

export function withoutUrl(settings: DerpSettings, url: string): SetDerpRequestBody {
  const next = cloneSettings(settings);

  next.urls = (next.urls ?? []).filter((existing) => existing !== url);

  return next;
}

export function withAutoUpdate(settings: DerpSettings, on: boolean): SetDerpRequestBody {
  const next = cloneSettings(settings);

  next.autoUpdate = on;

  return next;
}

export function withFrequency(settings: DerpSettings, frequency: string): SetDerpRequestBody {
  const next = cloneSettings(settings);

  next.updateFrequency = frequency;

  return next;
}

export function withServer(settings: DerpSettings, server: DerpServerSettings): SetDerpRequestBody {
  const next = cloneSettings(settings);

  next.server = { ...server };

  return next;
}

/** Adds a region, or replaces the one with the same id. */
export function withRegion(
  settings: DerpSettings,
  region: DerpCustomRegion,
  previousId?: number,
): SetDerpRequestBody {
  const next = cloneSettings(settings);
  const target = previousId ?? region.id;
  const regions = (next.regions ?? []).filter((existing) => existing.id !== target);

  regions.push(cloneRegion(region));
  regions.sort((left, right) => left.id - right.id);
  next.regions = regions;

  return next;
}

export function withoutRegion(settings: DerpSettings, id: number): SetDerpRequestBody {
  const next = cloneSettings(settings);

  next.regions = (next.regions ?? []).filter((existing) => existing.id !== id);

  return next;
}

export function urlError(value: string): string | null {
  if (value === "") {
    return "Enter a URL.";
  }

  try {
    const url = new URL(value);

    return (url.protocol === "https:" || url.protocol === "http:") && url.hostname !== ""
      ? null
      : "Use an http or https URL.";
  } catch {
    return "Use an http or https URL.";
  }
}

export function frequencyError(value: string): string | null {
  const seconds = durationSeconds(value);

  if (seconds === null) {
    return "Use a duration such as 3h or 30m.";
  }

  return seconds < minute ? "The maps cannot be refetched more often than every minute." : null;
}

const maxPort = 65_535;
const maxRegionId = 2_147_483_647;

export function regionIdError(value: string, taken: readonly number[]): string | null {
  const id = Number(value);

  if (!/^\d+$/v.test(value) || id < 1 || id > maxRegionId) {
    return "Use a positive number.";
  }

  return taken.includes(id) ? "Another region already uses this id." : null;
}

export function regionCodeError(value: string): string | null {
  return value === "" ? "Enter a code, such as sgp." : null;
}

const label = /^[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?$/iv;

export function hostNameError(value: string): string | null {
  if (value === "") {
    return "Enter the relay's host name.";
  }

  return value.split(".").every((part) => label.test(part))
    ? null
    : "Use a DNS name such as derp.example.com.";
}

export function portError(value: string): string | null {
  if (value === "") {
    return null;
  }

  const port = Number(value);

  return Number.isInteger(port) && port >= 0 && port <= maxPort
    ? null
    : "Use a port between 1 and 65535, or leave it empty.";
}

export function ipv4Error(value: string, allowNone: boolean): string | null {
  if (value === "" || (allowNone && value === "none") || isIpv4(value)) {
    return null;
  }

  return allowNone ? "Use an IPv4 address, none, or leave it empty." : "Use an IPv4 address.";
}

export function ipv6Error(value: string, allowNone: boolean): string | null {
  if (value === "" || (allowNone && value === "none") || isIpv6(value)) {
    return null;
  }

  return allowNone ? "Use an IPv6 address, none, or leave it empty." : "Use an IPv6 address.";
}

const hostPort = /^(?:\[[^\]]+\]|[^:]*):(?<port>\d{1,5})$/v;

export function stunAddrError(value: string): string | null {
  const match = hostPort.exec(value);

  if (match === null) {
    return "Use host:port, such as 0.0.0.0:3478.";
  }

  return Number(match.groups?.["port"]) <= maxPort ? null : "The port must be at most 65535.";
}

/** The relay as the form edits it: every field a string. */
export interface RelayDraft {
  /** Identity of the row while the form edits it; not sent. */
  readonly key: string;
  readonly hostName: string;
  readonly name: string;
  readonly ipv4: string;
  readonly ipv6: string;
  readonly derpPort: string;
  readonly stunPort: string;
  readonly stunOnly: boolean;
  readonly canPort80: boolean;
}

export function relayDraft(relay?: DerpRelay): RelayDraft {
  return {
    key: crypto.randomUUID(),
    hostName: relay?.hostName ?? "",
    name: relay?.name ?? "",
    ipv4: relay?.ipv4 ?? "",
    ipv6: relay?.ipv6 ?? "",
    derpPort: relay?.derpPort === undefined || relay.derpPort === 0 ? "" : String(relay.derpPort),
    stunPort: relay?.stunPort === undefined || relay.stunPort === 0 ? "" : String(relay.stunPort),
    stunOnly: relay?.stunOnly ?? false,
    canPort80: relay?.canPort80 ?? false,
  };
}

export function relayFromDraft(draft: RelayDraft): DerpRelay {
  const relay: DerpRelay = { hostName: draft.hostName.trim() };
  const name = draft.name.trim();
  const ipv4 = draft.ipv4.trim();
  const ipv6 = draft.ipv6.trim();

  if (name !== "") {
    relay.name = name;
  }

  if (ipv4 !== "") {
    relay.ipv4 = ipv4;
  }

  if (ipv6 !== "") {
    relay.ipv6 = ipv6;
  }

  if (draft.derpPort !== "") {
    relay.derpPort = Number(draft.derpPort);
  }

  if (draft.stunPort !== "") {
    relay.stunPort = Number(draft.stunPort);
  }

  if (draft.stunOnly) {
    relay.stunOnly = true;
  }

  if (draft.canPort80) {
    relay.canPort80 = true;
  }

  return relay;
}

/** What is wrong with each field of a relay draft; empty when nothing is. */
export type FieldErrors<Draft> = Partial<Record<keyof Draft, string>>;

export function relayFieldErrors(draft: RelayDraft): FieldErrors<RelayDraft> {
  const errors: FieldErrors<RelayDraft> = {};
  const checks: readonly (readonly [keyof RelayDraft, string | null])[] = [
    ["hostName", hostNameError(draft.hostName.trim())],
    ["ipv4", ipv4Error(draft.ipv4.trim(), true)],
    ["ipv6", ipv6Error(draft.ipv6.trim(), true)],
    ["derpPort", portError(draft.derpPort.trim())],
    ["stunPort", portError(draft.stunPort.trim())],
  ];

  for (const [field, error] of checks) {
    if (error !== null) {
      errors[field] = error;
    }
  }

  return errors;
}

/** The first thing wrong with a relay draft, or null. */
export function relayError(draft: RelayDraft): string | null {
  return firstError(relayFieldErrors(draft));
}

export function firstError<Draft>(errors: FieldErrors<Draft>): string | null {
  const first = Object.values(errors).find((error) => typeof error === "string");

  return typeof first === "string" ? first : null;
}

/** The name a relay is published under: its own, or its host name. */
export function relayPublishedName(draft: RelayDraft): string {
  const name = draft.name.trim();

  return name === "" ? draft.hostName.trim() : name;
}

/** A name two relays of a region share, or null. */
export function duplicateRelayName(relays: readonly RelayDraft[]): string | null {
  const seen = new Set<string>();

  for (const relay of relays) {
    const name = relayPublishedName(relay);

    if (name !== "" && seen.has(name)) {
      return name;
    }

    seen.add(name);
  }

  return null;
}

/** The embedded relay as the form edits it. */
export interface ServerDraft {
  readonly regionId: string;
  readonly regionCode: string;
  readonly regionName: string;
  readonly verifyClients: boolean;
  readonly stunAddr: string;
  readonly ipv4: string;
  readonly ipv6: string;
}

/** What the config file gives the embedded relay when nothing is set. */
const defaultRegionId = 999;

export function serverDraft(server: DerpServerSettings): ServerDraft {
  return {
    regionId: String(server.regionId ?? defaultRegionId),
    regionCode: server.regionCode ?? "headscale",
    regionName: server.regionName ?? "",
    verifyClients: server.verifyClients ?? true,
    stunAddr: server.stunAddr ?? "0.0.0.0:3478",
    ipv4: server.ipv4 ?? "",
    ipv6: server.ipv6 ?? "",
  };
}

export function serverFromDraft(draft: ServerDraft, enabled: boolean): DerpServerSettings {
  return {
    enabled,
    regionId: Number(draft.regionId),
    regionCode: draft.regionCode.trim(),
    regionName: draft.regionName.trim(),
    verifyClients: draft.verifyClients,
    stunAddr: draft.stunAddr.trim(),
    ipv4: draft.ipv4.trim(),
    ipv6: draft.ipv6.trim(),
  };
}

export function serverFieldErrors(
  draft: ServerDraft,
  taken: readonly number[],
): FieldErrors<ServerDraft> {
  const errors: FieldErrors<ServerDraft> = {};
  const checks: readonly (readonly [keyof ServerDraft, string | null])[] = [
    ["regionId", regionIdError(draft.regionId.trim(), taken)],
    ["regionCode", regionCodeError(draft.regionCode.trim())],
    ["stunAddr", stunAddrError(draft.stunAddr.trim())],
    ["ipv4", ipv4Error(draft.ipv4.trim(), false)],
    ["ipv6", ipv6Error(draft.ipv6.trim(), false)],
  ];

  for (const [field, error] of checks) {
    if (error !== null) {
      errors[field] = error;
    }
  }

  return errors;
}

export function serverError(draft: ServerDraft, taken: readonly number[]): string | null {
  return firstError(serverFieldErrors(draft, taken));
}

/** The ids the custom regions use, which the embedded relay and a new region must avoid. */
export function customRegionIds(settings: DerpSettings): number[] {
  return settings.regions.map((region) => region.id);
}
