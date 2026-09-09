import type { NodePreferences, UpdateNodePreferencesRequestBody } from "~/api/schema.gen.ts";

/** The one line an operator runs on the machine to hand its local API to the tailnet admin. */
export const remoteConfigCommand = "tailscale set --remote-config";

/** The switches, in the order the section and the dialog list them. */
export const preferenceSwitches = [
  { key: "acceptRoutes", label: "Accept routes" },
  { key: "acceptDns", label: "Accept DNS" },
  { key: "advertiseExitNode", label: "Offer to be an exit node" },
  { key: "exitNodeAllowLanAccess", label: "Reach the local network while using an exit node" },
  { key: "advertiseConnector", label: "Offer to be an app connector" },
  { key: "runSsh", label: "Run Tailscale SSH" },
  { key: "shieldsUp", label: "Block incoming traffic" },
  { key: "postureChecking", label: "Report posture" },
  { key: "autoUpdateCheck", label: "Check for client updates" },
  { key: "autoUpdateApply", label: "Apply client updates" },
] as const;

export type PreferenceSwitch = (typeof preferenceSwitches)[number]["key"];

/**
 * The changed fields only. The endpoint takes a partial: a field the body leaves out is a field the
 * client keeps, so sending the whole set back would overwrite whatever the machine's own owner
 * changed while the dialog was open.
 */
export function preferenceChanges(
  draft: NodePreferences,
  current: NodePreferences,
): UpdateNodePreferencesRequestBody {
  const changes: UpdateNodePreferencesRequestBody = {};

  for (const { key } of preferenceSwitches) {
    if (draft[key] !== current[key]) {
      changes[key] = draft[key];
    }
  }

  if (draft.hostname.trim() !== current.hostname) {
    changes.hostname = draft.hostname.trim();
  }

  if (draft.exitNode.trim() !== current.exitNode) {
    changes.exitNode = draft.exitNode.trim();
  }

  if (!sameRoutes(draft.advertiseRoutes, current.advertiseRoutes)) {
    changes.advertiseRoutes = [...draft.advertiseRoutes];
  }

  return changes;
}

/** The client masks and dedupes the prefixes itself, so only the set matters, not the order. */
function sameRoutes(draft: readonly string[], current: readonly string[]): boolean {
  return (
    draft.length === current.length && draft.toSorted().join(",") === current.toSorted().join(",")
  );
}

/** Whether anything at all would be sent, which is what holds the dialog's save button. */
export function hasPreferenceChanges(changes: UpdateNodePreferencesRequestBody): boolean {
  return Object.keys(changes).length > 0;
}

/** The exit node in use, by stable id or address; empty means the machine routes for itself. */
export function exitNodeLabel(preferences: NodePreferences): string {
  return preferences.exitNode === "" ? "None" : preferences.exitNode;
}
