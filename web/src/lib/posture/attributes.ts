/**
 * The attributes the server derives for every node, as `hscontrol/types/posture.go` names them,
 * with what each holds. `custom:` attributes are whatever an operator set on the machine, so any
 * name goes there; `ip:` attributes come from where the node connects.
 */
export interface AttributeInfo {
  readonly name: string;
  readonly doc: string;
  /** What the attribute holds, which decides what a comparison against it can mean. */
  readonly type: "string" | "version" | "bool" | "list" | "address" | "country";
  /** The values the attribute is known to take, when there is a fixed set. */
  readonly values?: readonly string[];
}

export const osValues = ["linux", "windows", "macos", "ios", "android", "tvos", "freebsd"] as const;

export const nodeAttributes: readonly AttributeInfo[] = [
  {
    name: "node:os",
    doc: "The operating system the client reports, lowercase.",
    type: "string",
    values: osValues,
  },
  {
    name: "node:osVersion",
    doc: "The operating system version the client reports.",
    type: "version",
  },
  {
    name: "node:tsVersion",
    doc: "The Tailscale client version without its build suffix, such as 1.86.2.",
    type: "version",
  },
  {
    name: "node:tsReleaseTrack",
    doc: "stable for an even minor version, unstable for an odd one.",
    type: "string",
    values: ["stable", "unstable"],
  },
  { name: "node:tsAutoUpdate", doc: "Whether the client has auto update on.", type: "bool" },
  {
    name: "node:serialNumber",
    doc: "The hardware serial numbers the client found. A list; == and IN match any of them.",
    type: "list",
  },
  { name: "node:hostname", doc: "The hostname the client reports.", type: "string" },
  { name: "node:machine", doc: "The CPU architecture, such as amd64 or arm64.", type: "string" },
  { name: "node:distro", doc: "The Linux distribution, such as ubuntu or nixos.", type: "string" },
  { name: "node:distroVersion", doc: "The Linux distribution version.", type: "version" },
  { name: "node:deviceModel", doc: "The device model the client reports.", type: "string" },
  {
    name: "node:package",
    doc: "How the client was installed, such as deb or brew.",
    type: "string",
  },
  {
    name: "node:tagged",
    doc: "Whether the node is tagged rather than owned by a user.",
    type: "bool",
  },
];

export const ipAttributes: readonly AttributeInfo[] = [
  {
    name: "ip:address",
    doc: "The address the node connects from. A string in CIDR form matches a range.",
    type: "address",
  },
  {
    name: "ip:country",
    doc: "The two-letter country code of the address, when the server has a GeoIP database.",
    type: "country",
  },
];

export const knownAttributes: readonly AttributeInfo[] = [...nodeAttributes, ...ipAttributes];

export function attributeInfo(name: string): AttributeInfo | undefined {
  return knownAttributes.find((attribute) => attribute.name === name);
}
