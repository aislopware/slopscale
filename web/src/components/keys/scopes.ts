import type { Me, Scope } from "~/auth/me.ts";
import type { PickerItem } from "~/components/ui/multi-picker.tsx";

/** Every scope a key may carry, in the server's order, with what it unlocks. */
export const scopeOptions: readonly { readonly scope: Scope; readonly label: string }[] = [
  { scope: "all", label: "Everything" },
  { scope: "all:read", label: "Read everything" },
  { scope: "devices:core", label: "Machines" },
  { scope: "devices:core:read", label: "Machines (read)" },
  { scope: "devices:routes", label: "Routes and networks" },
  { scope: "devices:routes:read", label: "Routes and networks (read)" },
  { scope: "devices:posture_attributes", label: "Posture attributes" },
  { scope: "devices:posture_attributes:read", label: "Posture attributes (read)" },
  { scope: "users", label: "Users" },
  { scope: "users:read", label: "Users (read)" },
  { scope: "auth_keys", label: "Pre-auth keys" },
  { scope: "auth_keys:read", label: "Pre-auth keys (read)" },
  { scope: "oauth_keys", label: "OAuth clients" },
  { scope: "oauth_keys:read", label: "OAuth clients (read)" },
  { scope: "policy_file", label: "Policy and access" },
  { scope: "policy_file:read", label: "Policy and access (read)" },
  { scope: "dns", label: "DNS" },
  { scope: "dns:read", label: "DNS (read)" },
  { scope: "services", label: "Services" },
  { scope: "services:read", label: "Services (read)" },
  { scope: "feature_settings", label: "Settings" },
  { scope: "feature_settings:read", label: "Settings (read)" },
  { scope: "webhooks", label: "Webhooks" },
  { scope: "webhooks:read", label: "Webhooks (read)" },
  { scope: "logs:configuration", label: "Audit log and streaming" },
  { scope: "logs:configuration:read", label: "Audit log (read)" },
];

export function scopeLabel(scope: string): string {
  return scopeOptions.find((option) => option.scope === scope)?.label ?? scope;
}

/** Scopes that mint machine credentials, which the server only allows tagged. */
const taggedScopes: ReadonlySet<string> = new Set(["devices:core", "auth_keys", "all"]);

/** Whether the picked scopes oblige the credential to carry tags. */
export function needsTags(scopes: readonly string[]): boolean {
  return scopes.some((scope) => taggedScopes.has(scope));
}

/** The scopes the caller may hand to a key: only what it holds itself, since the server narrows. */
export function scopeItems(me: Me): PickerItem[] {
  return scopeOptions
    .filter((option) => me.permissions[option.scope] === true)
    .map((option) => ({ value: option.scope, label: option.label, hint: option.scope }));
}

/** Combines scopes the caller holds with scopes already assigned to an existing credential. */
export function scopeItemsWithExisting(
  me: Me | undefined,
  existing: readonly string[],
): PickerItem[] {
  const base = me === undefined ? [] : scopeItems(me);
  const baseValues = new Set(base.map((item) => item.value));
  const extra: PickerItem[] = [];
  for (const scope of existing) {
    if (!baseValues.has(scope)) {
      extra.push({ value: scope, label: scopeLabel(scope), hint: scope });
    }
  }
  return [...base, ...extra];
}
