import type { MethodResponse } from "openapi-react-query";

import { api } from "~/api/client.ts";

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
  | "policy_file"
  | "policy_file:read"
  | "feature_settings"
  | "feature_settings:read"
  | "users"
  | "users:read";

export type Me = MethodResponse<typeof api, "get", "/api/v1/whoami">;

export const meQuery = api.queryOptions("get", "/api/v1/whoami");

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

  return me.role;
}

/** The overview greeting: a user's name, or what kind of credential is signed in. */
export function greeting(me: Me): string {
  if (me.user !== undefined) {
    return `Welcome back, ${displayName(me)}`;
  }

  return me.kind === "local"
    ? "Signed in over the local socket"
    : "Signed in with an all-access API key";
}
