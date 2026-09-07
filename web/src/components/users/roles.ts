/** The roles the server accepts, most to least privileged. */
export const userRoles = [
  "owner",
  "admin",
  "network-admin",
  "it-admin",
  "auditor",
  "member",
] as const;

export type UserRole = (typeof userRoles)[number];

export interface RoleOption {
  readonly value: UserRole;
  readonly label: string;
  readonly description: string;
}

/** One line per role, shown in the role select so operators can pick without the docs. */
export const roleOptions: readonly RoleOption[] = [
  {
    value: "owner",
    label: "owner",
    description: "Full control, and the only role that can transfer ownership.",
  },
  { value: "admin", label: "admin", description: "Everything except the owner." },
  {
    value: "network-admin",
    label: "network-admin",
    description: "Policy, DNS and route approval; reads everything else.",
  },
  {
    value: "it-admin",
    label: "it-admin",
    description: "Users, devices, keys and settings; reads policy and routes.",
  },
  { value: "auditor", label: "auditor", description: "Reads everything, changes nothing." },
  { value: "member", label: "member", description: "No admin access." },
];

/** Narrows the role string the API returns; unknown roles fall back to the least privileged. */
export function toRole(role: string): UserRole {
  return userRoles.find((known) => known === role) ?? "member";
}
