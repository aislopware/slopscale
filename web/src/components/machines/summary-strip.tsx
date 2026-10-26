import { DesktopIcon, GlobeHemisphereWestIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { plural } from "~/components/overview/plural.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import {
  approvedSubnets,
  isExitNode,
  isTagged,
  nodeName,
  nodeStatus,
  ownerLabel,
  pendingRouteCount,
} from "~/lib/node.ts";

const iconSize = 16;

/**
 * The strip across the top of a Zero Trust detail page ("Profile: Default · Enabled · 0 devices", a
 * Mesh node's "Mesh IP: – · Offline"): the machine's name, which is the page's title, its
 * connection badge, its tailnet address, whose it is, when it was last seen and what it does for
 * the tailnet, so the cards under it can each be about one thing. Whose it is sits apart at the
 * right end.
 */
export function MachineSummary({ node }: { readonly node: Node }): ReactElement {
  const ipv4 = node.ipAddresses.find((address) => !address.includes(":"));
  const subnets = approvedSubnets(node).length;
  const pending = pendingRouteCount(node);

  return (
    <div className="flex min-h-12 flex-col gap-3 rounded-lg border border-kumo-hairline bg-kumo-base px-4 py-3 sm:flex-row sm:items-center sm:gap-4">
      {/* Clipped sideways so a divider drawn in the gap before an item vanishes when that item
          starts a line, rather than dangling at the end of the line above. */}
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-2 overflow-x-clip">
        <span className="flex min-w-0 items-center gap-2">
          <DesktopIcon size={iconSize} aria-hidden className="shrink-0 text-kumo-subtle" />
          <h1 className="truncate text-base font-medium text-kumo-default">{nodeName(node)}</h1>
        </span>
        <StatusBadge status={nodeStatus(node)} />
        <span className="relative flex items-center gap-2 before:absolute before:top-1/2 before:-left-1.5 before:h-4 before:-translate-y-1/2 before:border-l before:border-kumo-line">
          <GlobeHemisphereWestIcon size={iconSize} aria-hidden className="text-kumo-subtle" />
          <span className="text-kumo-subtle">Tailnet IP</span>
          {ipv4 === undefined ? (
            <span className="text-kumo-subtle">–</span>
          ) : (
            <CopyText value={ipv4} label="Copy tailnet IP" className="text-kumo-default" />
          )}
        </span>
        {node.ephemeral ? <Badge tone="neutral">Ephemeral</Badge> : null}
        {node.globalExitNode ? <Badge tone="info">Global exit node</Badge> : null}
        {!node.globalExitNode && isExitNode(node) ? <Badge tone="neutral">Exit node</Badge> : null}
        {subnets === 0 ? null : <Badge tone="neutral">{plural(subnets, "subnet route")}</Badge>}
        {pending === 0 ? null : (
          <Badge tone="warning">{`${plural(pending, "route")} waiting`}</Badge>
        )}
        {node.online ? null : (
          <span className="text-sm text-kumo-subtle">
            {"Last seen "}
            <RelativeTime value={node.lastSeen} />
          </span>
        )}
      </div>
      {/* Whose it is answers a different question from the facts, so it keeps its own column at
          the right rather than wrapping in among them; on a phone it goes under them. */}
      <div className="flex min-w-0 items-center border-t border-kumo-line pt-3 sm:max-w-[40%] sm:shrink-0 sm:justify-end sm:self-stretch sm:border-t-0 sm:border-l sm:pt-0 sm:pl-4">
        <Owner node={node} />
      </div>
    </div>
  );
}

/** Whose it is: the tags of a tagged machine, the person otherwise. */
function Owner({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return <TagList tags={node.tags} size="sm" className="sm:justify-end" />;
  }

  const label = ownerLabel(node);

  return (
    <span className="flex min-w-0 items-center gap-1.5 text-sm text-kumo-default">
      <Avatar name={label} id={node.user.id} src={node.user.profilePicUrl} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}
