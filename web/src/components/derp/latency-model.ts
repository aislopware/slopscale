import type {
  DerpLatencyMachine,
  DerpLatencyRegion,
  DerpLatencyReport,
  NodeDerpLatency,
  NodeNetInfo,
} from "~/api/schema.gen.ts";
import type { Tone } from "~/components/ui/status.tsx";

/** What a measurement shows when nothing measured it; the server sends 0 for both. */
export const noValue = "—";

const msDigits = 1;

/** A round trip to one decimal, for a column whose heading already carries the unit. */
export function msValue(ms: number): string {
  return ms > 0 ? ms.toFixed(msDigits) : noValue;
}

/** The same round trip with its unit, for a column that is not headed "(ms)". */
export function msLabel(ms: number): string {
  return ms > 0 ? `${ms.toFixed(msDigits)} ms` : noValue;
}

/** The best and the worst round trip of a region, as one cell. */
export function rangeValue(region: DerpLatencyRegion): string {
  return region.samples === 0 ? noValue : `${msValue(region.minMs)} / ${msValue(region.maxMs)}`;
}

/** The widest bar in the "Preferred by" column; every other bar is a share of it. */
export function maxPreferredBy(regions: readonly DerpLatencyRegion[]): number {
  return regions.reduce((most, region) => Math.max(most, region.preferredBy), 0);
}

const wholeBar = 100;

/** How much of the widest bar a count fills, as a percentage. */
export function barPercent(value: number, max: number): number {
  return max <= 0 ? 0 : Math.min((value / max) * wholeBar, wholeBar);
}

/** The region a machine homes on, named by the report; absent while the client has not picked one. */
export function machineHome(
  report: DerpLatencyReport,
  machine: DerpLatencyMachine,
): DerpLatencyRegion | undefined {
  return report.regions.find((region) => region.regionId === machine.preferredDerp);
}

export interface LatencyMachineRow {
  readonly machine: DerpLatencyMachine;
  /** The region it homes on, looked up once here rather than by the cell that names it. */
  readonly home: DerpLatencyRegion | undefined;
}

/** The report's machines as table rows, worst round trip first, as the server ordered them. */
export function machineRows(report: DerpLatencyReport): LatencyMachineRow[] {
  return report.machines.map((machine) => ({ machine, home: machineHome(report, machine) }));
}

/**
 * What kind of NAT the client sits behind. A hard NAT maps a different port per destination, so
 * nothing on the outside can guess where to send the first packet and the traffic goes by relay.
 */
export function natLabel(hard: boolean | null): string {
  if (hard === null) {
    return "Unknown";
  }

  return hard ? "Hard" : "Easy";
}

export function natTone(hard: boolean | null): Tone {
  if (hard === null) {
    return "neutral";
  }

  return hard ? "warning" : "success";
}

/** A flag the client reports only once it has tested it. */
export function yesNo(value: boolean | null): string {
  if (value === null) {
    return "Unknown";
  }

  return value ? "Yes" : "No";
}

const linkLabels: Record<string, string> = {
  wired: "Wired",
  wifi: "Wi-Fi",
  mobile: "Mobile",
};

/** How the client reaches the network; it sends nothing at all when it cannot tell. */
export function linkLabel(linkType: string): string {
  return linkLabels[linkType] ?? (linkType === "" ? "Unknown" : linkType);
}

/** The port mapping protocols the client saw on its LAN. */
export function portMapProtocols(netInfo: NodeNetInfo): string[] {
  const seen: string[] = [];

  if (netInfo.upnp === true) {
    seen.push("UPnP");
  }

  if (netInfo.pmp === true) {
    seen.push("NAT-PMP");
  }

  if (netInfo.pcp === true) {
    seen.push("PCP");
  }

  return seen;
}

/** The protocols that answered, or what the client managed to say when none did. */
export function portMapLabel(netInfo: NodeNetInfo): string {
  const seen = portMapProtocols(netInfo);

  if (seen.length > 0) {
    return seen.join(", ");
  }

  if (netInfo.havePortMap) {
    return "Open";
  }

  if (netInfo.upnp === null && netInfo.pmp === null && netInfo.pcp === null) {
    return "Unknown";
  }

  return "None found";
}

/** The client's home region among its own measurements, when it reported one. */
export function homeLatency(netInfo: NodeNetInfo): NodeDerpLatency | undefined {
  if (netInfo.preferredDerp === 0) {
    return undefined;
  }

  return netInfo.latency.find((region) => region.regionId === netInfo.preferredDerp);
}

/** Sorts a region nothing measured after every measured one, without reaching infinity. */
const unmeasuredRank = Number.MAX_SAFE_INTEGER;

function rank(ms: number): number {
  return ms > 0 ? ms : unmeasuredRank;
}

function compareByMs(left: NodeDerpLatency, right: NodeDerpLatency): number {
  const gap = rank(left.ms) - rank(right.ms);

  return gap === 0 ? left.code.localeCompare(right.code) : gap;
}

/** Every region but the home one, nearest first, with the unmeasured ones last. */
export function otherLatencies(netInfo: NodeNetInfo): NodeDerpLatency[] {
  return netInfo.latency
    .filter((region) => region.regionId !== netInfo.preferredDerp)
    .toSorted(compareByMs);
}
