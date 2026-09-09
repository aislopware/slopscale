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
    label: "Owner",
    description: "Full control, and the only role that can transfer ownership.",
  },
  { value: "admin", label: "Admin", description: "Everything except the owner." },
  {
    value: "network-admin",
    label: "Network admin",
    description: "Policy, DNS and route approval. Reads everything else.",
  },
  {
    value: "it-admin",
    label: "IT admin",
    description: "Users, machines, keys and settings. Reads policy and routes.",
  },
  { value: "auditor", label: "Auditor", description: "Reads everything, changes nothing." },
  { value: "member", label: "Member", description: "No admin access." },
];

/** Narrows the role string the API returns; unknown roles fall back to the least privileged. */
export function toRole(role: string): UserRole {
  return userRoles.find((known) => known === role) ?? "member";
}

/** Roles that reach the console's admin surfaces; the "Admins" filter on the users page. */
const adminRoles = new Set<UserRole>(["owner", "admin", "network-admin", "it-admin"]);

/** Sentence-case name for a role, for badges and menus. */
export function roleName(role: string): string {
  return roleOptions.find((option) => option.value === toRole(role))?.label ?? "Member";
}

export function isAdminRole(role: string): boolean {
  return adminRoles.has(toRole(role));
}
