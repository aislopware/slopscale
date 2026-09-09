import type { MethodResponse } from "openapi-react-query";

import { api } from "~/api/client.ts";
import { sharedStaleTime } from "~/api/queries.ts";
import { roleName } from "~/components/users/roles.ts";

/** The scopes the server knows; `Whoami.permissions` has one entry per scope. */
export type Scope =
  | "all"
  | "all:read"
  | "auth_keys"
  | "auth_keys:read"
  | "oauth_keys"
  | "oauth_keys:read"
  | "devices:core"
  | "devices:core:read"
  | "devices:routes"
  | "devices:routes:read"
  | "devices:posture_attributes"
  | "devices:posture_attributes:read"
  | "policy_file"
  | "policy_file:read"
  | "feature_settings"
  | "feature_settings:read"
  | "users"
  | "users:read"
  | "dns"
  | "dns:read"
  | "webhooks"
  | "webhooks:read"
  | "logs:configuration"
  | "logs:configuration:read"
  | "services"
  | "services:read";

export type Me = MethodResponse<typeof api, "get", "/api/v1/whoami">;

/**
 * Who is signed in. The route guard, the account menu and the sidebar all read it, so it keeps the
 * shared stale time; a role or profile change refetches it with `staleTime: 0`.
 */
export const meQuery = api.queryOptions("get", "/api/v1/whoami", undefined, {
  staleTime: sharedStaleTime,
});

/** What the sign-in page may offer; public, so it loads before any credential. */
export const consoleAuthQuery = api.queryOptions("get", "/api/v1/auth/console");

export function can(me: Me, scope: Scope): boolean {
  return me.permissions[scope] === true;
}

/** True for a member-role key: it may still act on the caller's own nodes. */
export function isMember(me: Me): boolean {
  return me.role === "member" || me.role === "";
}

export function displayName(me: Me): string {
  if (me.user !== undefined) {
    return me.user.displayName === "" ? me.user.name : me.user.displayName;
  }

  if (me.kind === "local") {
    return "Local socket";
  }

  return me.allAccess ? "All-access key" : "API key";
}

/** The role bounding the caller, or null for a credential without a user (roles bound users). */
export function roleLabel(me: Me): string | null {
  if (me.user === undefined || me.role === "") {
    return null;
  }

  return roleName(me.role);
}
