import type { NodePreferences, UpdateNodePreferencesRequestBody } from "~/api/schema.gen.ts";

/** The one line an operator runs on the machine to hand its local API to the tailnet admin. */
export const remoteConfigCommand = "tailscale set --remote-config";

/**
 * The switches, in the order the section and the dialog list them. The label is what fits beside a
 * value in half a column; what the setting actually does is the hint, which both the section and
 * the dialog put one hover, tap or focus away rather than on the line.
 */
export const preferenceSwitches = [
  {
    key: "acceptRoutes",
    label: "Accept routes",
    hint: "Use the subnet routes other machines advertise instead of ignoring them.",
  },
  {
    key: "acceptDns",
    label: "Accept DNS",
    hint: "Take the tailnet's DNS configuration, including MagicDNS names.",
  },
  {
    key: "advertiseExitNode",
    label: "Exit node",
    hint: "Offer this machine as the way out to the internet for the rest of the tailnet.",
  },
  {
    key: "exitNodeAllowLanAccess",
    label: "Exit node LAN access",
    hint: "Reach the machine's own local network while its traffic goes through an exit node.",
  },
  {
    key: "advertiseConnector",
    label: "App connector",
    hint: "Offer this machine as an app connector, resolving domains and routing what it learns.",
  },
  { key: "runSsh", label: "Run SSH", hint: "Answer Tailscale SSH sessions the policy allows." },
  {
    key: "shieldsUp",
    label: "Shields up",
    hint: "Refuse every incoming connection; the machine can still start its own.",
  },
  {
    key: "postureChecking",
    label: "Report posture",
    hint: "Send the device attributes the tailnet's posture checks read.",
  },
  {
    key: "autoUpdateCheck",
    label: "Check for updates",
    hint: "Look for a newer Tailscale client and say so.",
  },
  {
    key: "autoUpdateApply",
    label: "Apply updates",
    hint: "Install a newer Tailscale client itself, which is what lets the tailnet update it.",
  },
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

/**
 * Whether the dialog may send what it has. An empty hostname is a valid preference — it tells the
 * client to use the machine's own OS name — so it never holds the form; only a wrong row in a list
 * does, along with there being nothing to send.
 */
export function canSavePreferences(
  changes: UpdateNodePreferencesRequestBody,
  invalidLists: boolean,
): boolean {
  return !invalidLists && hasPreferenceChanges(changes);
}

/** The exit node in use, by stable id or address; empty means the machine routes for itself. */
export function exitNodeLabel(preferences: NodePreferences): string {
  return preferences.exitNode === "" ? "None" : preferences.exitNode;
}
