import type { ReactElement } from "react";

import { Badge } from "~/components/ui/badge.tsx";
import type { BadgeTone } from "~/components/ui/badge.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import type { NodeStatus } from "~/lib/node.ts";

/**
 * A machine's connection state has a badge mapping of its own, the one Cloudflare's Mesh draws for
 * a node: a disconnected machine is the solid inverted pill (black in light mode, white in dark),
 * as prominent as a connected one's green, because "not on the tailnet right now" is the first
 * thing an operator looks for. The states that need someone to act keep their warning and danger
 * tints.
 */
const styles: Record<NodeStatus, { tone: Tone; badge: BadgeTone; label: string }> = {
  online: { tone: "success", badge: "success", label: "Connected" },
  offline: { tone: "neutral", badge: "inverted", label: "Disconnected" },
  pending: { tone: "warning", badge: "warning", label: "Needs approval" },
  expired: { tone: "danger", badge: "danger", label: "Key expired" },
  suspended: { tone: "danger", badge: "danger", label: "Suspended" },
};

export function statusLabel(status: NodeStatus): string {
  return styles[status].label;
}

export function statusTone(status: NodeStatus): Tone {
  return styles[status].tone;
}

/** The one status indicator for a machine: the same wording everywhere, in its connection badge. */
export function StatusBadge({ status }: { readonly status: NodeStatus }): ReactElement {
  return <Badge tone={styles[status].badge}>{statusLabel(status)}</Badge>;
}
