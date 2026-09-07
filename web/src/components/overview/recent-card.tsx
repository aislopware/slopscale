import { Link as KumoLink } from "@cloudflare/kumo/components/link";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { Card, CardHeader, CardTitle } from "~/components/ui/card.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { StatusDot } from "~/components/ui/status-dot.tsx";
import { nodeName, nodeStatus, ownerLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

/** Enough rows to show what is happening without repeating the machines page. */
const maxRows = 8;

function seenAt(node: Node): number {
  return parseTime(node.lastSeen)?.getTime() ?? 0;
}

function byActivity(left: Node, right: Node): number {
  if (left.online !== right.online) {
    return left.online ? -1 : 1;
  }

  return seenAt(right) - seenAt(left);
}

export interface RecentCardProps {
  readonly nodes: readonly Node[];
}

/** The machines that were connected most recently, online ones first. */
export function RecentCard({ nodes }: RecentCardProps): ReactElement | null {
  if (nodes.length === 0) {
    return null;
  }

  const recent = nodes.toSorted(byActivity).slice(0, maxRows);

  return (
    <Card>
      <CardHeader className="items-center">
        <CardTitle>Recently active</CardTitle>
        <KumoLink href="/machines" variant="plain">
          View all
        </KumoLink>
      </CardHeader>
      <div className="flex flex-col divide-y divide-kumo-line">
        {recent.map((node) => (
          <div key={node.id} className="flex items-center gap-3 px-5 py-3">
            <StatusDot status={nodeStatus(node)} />
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <Link
                to="/machines/$nodeId"
                params={{ nodeId: node.id }}
                className="block truncate font-medium text-kumo-default hover:underline"
              >
                {nodeName(node)}
              </Link>
              <p className="truncate text-sm text-kumo-subtle">{ownerLabel(node)}</p>
            </div>
            <span className="text-sm whitespace-nowrap text-kumo-subtle">
              <RelativeTime value={node.lastSeen} />
            </span>
          </div>
        ))}
      </div>
    </Card>
  );
}
