import type { ReactElement } from "react";

import { statusLabel, statusTone } from "~/components/ui/status-dot.tsx";
import { Status } from "~/components/ui/status.tsx";
import type { NodeStatus } from "~/lib/node.ts";

/** The one status indicator for a machine: a dot and the same wording everywhere. */
export function StatusBadge({ status }: { readonly status: NodeStatus }): ReactElement {
  return <Status tone={statusTone(status)}>{statusLabel(status)}</Status>;
}
