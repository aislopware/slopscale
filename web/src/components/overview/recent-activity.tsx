import { TagIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node } from "~/api/queries.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { OsMark } from "~/components/ui/os-mark.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section } from "~/components/ui/section.tsx";
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

/**
 * One machine: whose it is as an avatar, the person's blend or the tag mark, then the OS mark and
 * the name, so the list reads by owner and kind before a name is read.
 */
function ActivityRow({ node }: { readonly node: Node }): ReactElement {
  const [address] = node.ipAddresses;
  const owner = ownerLabel(node);

  return (
    <Link
      to="/machines/$nodeId"
      params={{ nodeId: node.id }}
      className="group flex items-center gap-3 px-5 py-2.5 text-kumo-default no-underline not-first:border-t not-first:border-kumo-hairline hover:bg-kumo-tint"
    >
      {isTagged(node) ? <Avatar name={owner} icon={TagIcon} /> : <Avatar name={owner} />}
      <span className="flex min-w-0 items-center gap-1.5">
        <OsMark os={node.os} version={node.osVersion} />
        <span className="min-w-0 truncate font-medium text-kumo-default group-hover:text-kumo-link group-hover:underline group-focus-visible:underline">
          {nodeName(node)}
        </span>
      </span>
      <span className="hidden min-w-0 truncate text-kumo-subtle sm:block">{owner}</span>
      <span className="ml-auto flex shrink-0 items-center gap-6 text-kumo-subtle">
        <span className="hidden font-mono text-[0.9em] lg:block">{address ?? ""}</span>
        <span className="w-32 text-right">
          {node.online ? "Connected" : <RelativeTime value={node.lastSeen} never="Never seen" />}
        </span>
      </span>
    </Link>
  );
}

export interface RecentActivityProps {
  readonly nodes: readonly Node[];
}

/** The machines that were connected most recently, online ones first; each row opens the machine. */
export function RecentActivity({ nodes }: RecentActivityProps): ReactElement | null {
  if (nodes.length === 0) {
    return null;
  }

  const recent = nodes.toSorted(byActivity).slice(0, maxRows);

  return (
    <Section
      title="Recently active"
      description="The machines that reached the control server most recently."
      bodyClassName="p-0"
      actions={
        <Link to="/machines" className="text-kumo-link hover:underline">
          View all machines
        </Link>
      }
    >
      {recent.map((node) => (
        <ActivityRow key={node.id} node={node} />
      ))}
    </Section>
  );
}
