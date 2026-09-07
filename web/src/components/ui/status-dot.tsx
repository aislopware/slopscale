import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import type { NodeStatus } from "~/lib/node.ts";

const styles: Record<NodeStatus, { dot: string; label: string }> = {
  online: { dot: "bg-kumo-success", label: "Connected" },
  offline: { dot: "bg-kumo-inactive", label: "Disconnected" },
  pending: { dot: "bg-kumo-warning", label: "Needs approval" },
  expired: { dot: "bg-kumo-danger", label: "Key expired" },
  suspended: { dot: "bg-kumo-danger", label: "Suspended" },
};

export function statusLabel(status: NodeStatus): string {
  return styles[status].label;
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
      <span
        aria-hidden
        className={cn("inline-block size-2 shrink-0 rounded-full", styles[status].dot, className)}
      />
      <span className="sr-only">{styles[status].label}</span>
    </span>
  );
}
