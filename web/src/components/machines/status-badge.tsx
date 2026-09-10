import type { ReactElement } from "react";

import { Status } from "~/components/ui/status.tsx";
import type { Tone } from "~/components/ui/status.tsx";
import type { NodeStatus } from "~/lib/node.ts";

const styles: Record<NodeStatus, { tone: Tone; label: string }> = {
  online: { tone: "success", label: "Connected" },
  offline: { tone: "neutral", label: "Disconnected" },
  pending: { tone: "warning", label: "Needs approval" },
  expired: { tone: "danger", label: "Key expired" },
  suspended: { tone: "danger", label: "Suspended" },
};

export function statusLabel(status: NodeStatus): string {
  return styles[status].label;
}

export function statusTone(status: NodeStatus): Tone {
  return styles[status].tone;
}

/** The one status indicator for a machine: the same wording everywhere, coloured by tone. */
export function StatusBadge({ status }: { readonly status: NodeStatus }): ReactElement {
  return <Status tone={statusTone(status)}>{statusLabel(status)}</Status>;
}
