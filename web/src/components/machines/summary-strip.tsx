import { GlobeHemisphereWestIcon } from "@phosphor-icons/react";
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
  nodeStatus,
  ownerLabel,
  pendingRouteCount,
} from "~/lib/node.ts";

const iconSize = 16;

/**
 * The strip across the top of a Zero Trust detail page ("Profile: Default · Enabled · 0 devices", a
 * Mesh node's "Mesh IP: – · Offline"): the machine's tailnet address, its state, whose it is, when
 * it was last seen and what it does for the tailnet, so the cards under it can each be about one
 * thing.
 */
export function MachineSummary({ node }: { readonly node: Node }): ReactElement {
  const ipv4 = node.ipAddresses.find((address) => !address.includes(":"));
  const subnets = approvedSubnets(node).length;
  const pending = pendingRouteCount(node);

  return (
    <div className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-kumo-hairline bg-kumo-base px-4 py-3">
      <span className="flex items-center gap-2">
        <GlobeHemisphereWestIcon size={iconSize} aria-hidden className="text-kumo-subtle" />
        <span className="text-base font-medium text-kumo-default">Tailnet IP:</span>
        {ipv4 === undefined ? (
          <span className="text-kumo-subtle">–</span>
        ) : (
          <CopyText value={ipv4} label="Copy tailnet IP" className="text-base text-kumo-default" />
        )}
      </span>
      <StatusBadge status={nodeStatus(node)} />
      {node.ephemeral ? <Badge tone="neutral">Ephemeral</Badge> : null}
      {node.globalExitNode ? <Badge tone="info">Global exit node</Badge> : null}
      {!node.globalExitNode && isExitNode(node) ? <Badge tone="neutral">Exit node</Badge> : null}
      {subnets === 0 ? null : <Badge tone="neutral">{plural(subnets, "subnet route")}</Badge>}
      {pending === 0 ? null : <Badge tone="warning">{`${plural(pending, "route")} waiting`}</Badge>}
      <span aria-hidden className="h-4 border-l border-kumo-line" />
      <Owner node={node} />
      {node.online ? null : (
        <span className="text-sm text-kumo-subtle">
          {"Last seen "}
          <RelativeTime value={node.lastSeen} />
        </span>
      )}
    </div>
  );
}

/** Whose it is: the tags of a tagged machine, the person otherwise. */
function Owner({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return <TagList tags={node.tags} size="sm" />;
  }

  const label = ownerLabel(node);

  return (
    <span className="flex min-w-0 items-center gap-1.5 text-sm text-kumo-default">
      <Avatar name={label} id={node.user.id} src={node.user.profilePicUrl} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}
