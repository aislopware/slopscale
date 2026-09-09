import type { ReactElement } from "react";

import { Dot } from "~/components/ui/status.tsx";
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

export function StatusDot({
  status,
  className,
}: {
  readonly status: NodeStatus;
  readonly className?: string;
}): ReactElement {
  return (
    <span className="inline-flex items-center">
      <Dot tone={styles[status].tone} {...(className === undefined ? {} : { className })} />
      <span className="sr-only">{styles[status].label}</span>
    </span>
  );
}
