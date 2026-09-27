import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { statusLabel } from "~/components/machines/status-badge.tsx";
import { MachineName } from "~/components/ui/machine-name.tsx";
import { Status } from "~/components/ui/status.tsx";
import { nodeName, nodeOwner } from "~/lib/node.ts";

/** A machine advertising a route: the link to it, whether it is connected, and whose it is. */
export function MachineLink({
  node,
  className,
}: {
  readonly node: Node;
  readonly className?: string;
}): ReactElement {
  return (
    <MachineName
      className={className}
      name={
        <span className="flex max-w-full min-w-0 items-center gap-2">
          <Link
            to="/machines/$nodeId"
            params={{ nodeId: node.id }}
            className="truncate hover:underline"
          >
            {nodeName(node)}
          </Link>
          <Status tone={node.online ? "success" : "neutral"} className="text-kumo-subtle">
            {statusLabel(node.online ? "online" : "offline")}
          </Status>
        </span>
      }
      owner={nodeOwner(node)}
    />
  );
}
