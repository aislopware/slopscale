import { CaretRightIcon, TagIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { OsMark } from "~/components/ui/os-mark.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { isTagged, nodeName, ownerLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

/** Enough rows to show what is happening without repeating the machines page. */
const maxRows = 6;

function seenAt(node: Node): number {
  return parseTime(node.lastSeen)?.getTime() ?? 0;
}

function byActivity(left: Node, right: Node): number {
  if (left.online !== right.online) {
    return left.online ? -1 : 1;
  }

  return seenAt(right) - seenAt(left);
}

/** Tags an owner column shows before it counts the rest. */
const maxOwnerTags = 2;

/**
 * One machine as a row of the account home's lists: whose it is as an avatar (the person's blend or
 * the tag mark), the OS mark and the name, the owner, then the address, when it was last seen and
 * the chevron that says the row opens the machine.
 */
function ActivityRow({ node }: { readonly node: Node }): ReactElement {
  const [address] = node.ipAddresses;
  const owner = ownerLabel(node);

  return (
    <li className="border-b border-kumo-line">
      <Link
        to="/machines/$nodeId"
        params={{ nodeId: node.id }}
        className="group flex min-h-14 items-center gap-3 rounded-md px-1 text-kumo-default no-underline outline-none hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus"
      >
        {isTagged(node) ? (
          <Avatar name={owner} icon={TagIcon} />
        ) : (
          <Avatar name={owner} id={node.user.id} src={node.user.profilePicUrl} />
        )}
        <span className="flex min-w-0 items-center gap-1.5">
          <OsMark os={node.os} version={node.osVersion} />
          <span className="min-w-0 truncate font-medium">{nodeName(node)}</span>
        </span>
        <span className="hidden min-w-0 truncate text-kumo-subtle sm:block">
          {isTagged(node) ? <TagList tags={node.tags} size="sm" max={maxOwnerTags} /> : owner}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-4 text-kumo-subtle">
          <span className="hidden font-mono text-xs tabular-nums xl:block">{address ?? ""}</span>
          <span className="text-right whitespace-nowrap">
            {node.online ? "Connected" : <RelativeTime value={node.lastSeen} never="Never seen" />}
          </span>
          <CaretRightIcon aria-hidden className="size-3.5" />
        </span>
      </Link>
    </li>
  );
}

export interface RecentActivityProps {
  readonly nodes: readonly Node[];
}

/**
 * The machines that were connected most recently, online ones first, as the account home lists its
 * domains: a quiet heading that leads to the whole list with the count beside it, then plain rows
 * divided by lines, each opening its machine.
 */
export function RecentActivity({ nodes }: RecentActivityProps): ReactElement | null {
  if (nodes.length === 0) {
    return null;
  }

  const recent = nodes.toSorted(byActivity).slice(0, maxRows);

  return (
    <section aria-labelledby="recently-active" className="flex flex-col gap-1">
      <div className="flex min-h-8 items-center justify-between gap-4">
        <h2 id="recently-active" className="text-sm font-normal text-kumo-subtle">
          Recently active
        </h2>
        <Link
          to="/machines"
          className="inline-flex items-center gap-1.5 text-sm text-kumo-subtle no-underline hover:text-kumo-default"
        >
          View all machines
          <Badge tone="neutral">{nodes.length}</Badge>
          <CaretRightIcon aria-hidden className="size-3" />
        </Link>
      </div>
      <ul className="flex flex-col">
        {recent.map((node) => (
          <ActivityRow key={node.id} node={node} />
        ))}
      </ul>
    </section>
  );
}
