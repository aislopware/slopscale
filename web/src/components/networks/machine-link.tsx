import { cn } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { statusLabel } from "~/components/machines/status-badge.tsx";
import { rowLinkClass } from "~/components/table/styles.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { isTagged, nodeName, ownerLabel } from "~/lib/node.ts";

/**
 * A machine carrying a route, on one line as the dashboard's connector cell has it: the name as an
 * underlined link in the text colour, whose it is (its first tag, or the person's avatar with the
 * name a hover away), and whether it is connected. A given name such as "localhost" tells neither
 * the machine nor its owner, so the owner stays beside it.
 */
export function MachineLink({
  node,
  className,
}: {
  readonly node: Node;
  readonly className?: string;
}): ReactElement {
  const owner = ownerLabel(node);

  return (
    <span className={cn("flex max-w-full min-w-0 items-center gap-2", className)}>
      <Link
        to="/machines/$nodeId"
        params={{ nodeId: node.id }}
        className={cn("min-w-0 truncate", rowLinkClass)}
      >
        {nodeName(node)}
      </Link>
      {isTagged(node) ? (
        <TagList tags={node.tags} size="sm" max={1} className="shrink-0 flex-nowrap" />
      ) : (
        <span title={owner} className="flex shrink-0">
          <Avatar name={owner} id={node.user.id} src={node.user.profilePicUrl} size="sm" />
        </span>
      )}
      <span
        className={cn(
          "shrink-0 text-xs whitespace-nowrap",
          node.online ? "text-kumo-success" : "text-kumo-subtle",
        )}
      >
        {statusLabel(node.online ? "online" : "offline")}
      </span>
    </span>
  );
}
