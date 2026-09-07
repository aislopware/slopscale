import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { statusLabel } from "~/components/ui/status-dot.tsx";
import type { NodeStatus } from "~/lib/node.ts";

const variants = {
  online: "success",
  offline: "neutral",
  pending: "warning",
  expired: "error",
  suspended: "error",
} as const satisfies Record<NodeStatus, "success" | "neutral" | "warning" | "error">;

/** The one status indicator for a machine: a dot badge with the same wording everywhere. */
export function StatusBadge({ status }: { readonly status: NodeStatus }): ReactElement {
  return (
    <Badge appearance="dot" variant={variants[status]}>
      {statusLabel(status)}
    </Badge>
  );
}
