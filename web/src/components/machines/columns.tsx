import { Badge } from "@cloudflare/kumo/components/badge";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { GlobeIcon, PathIcon, ShareNetworkIcon, StarIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import {
  approvedSubnets,
  isExitNode,
  isTagged,
  nodeName,
  nodeStatus,
  ownerLabel,
  pendingRoutes,
} from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

export const emptyUsers: readonly User[] = [];

const helper = createAppColumnHelper<Node>();

const statusOrder = { online: 0, pending: 1, offline: 2, expired: 3 } as const;

export const columns = helper.columns([
  helper.accessor((node) => nodeName(node), {
    id: "name",
    header: "Machine",
    enableSorting: true,
    cell: ({ row }) => <NameCell node={row.original} />,
    meta: { className: "w-[28%] min-w-56" },
  }),
  helper.accessor((node) => ownerLabel(node), {
    id: "owner",
    header: "Owner",
    enableSorting: true,
    cell: ({ row }) => <OwnerCell node={row.original} />,
  }),
  helper.accessor((node) => node.ipAddresses.join(" "), {
    id: "addresses",
    header: "Addresses",
    enableSorting: false,
    cell: ({ row }) => <AddressCell node={row.original} />,
    meta: { className: "hidden md:table-cell" },
  }),
  helper.accessor((node) => statusOrder[nodeStatus(node)], {
    id: "status",
    header: "Status",
    enableSorting: true,
    enableGlobalFilter: false,
    cell: ({ row }) => <StatusCell node={row.original} />,
  }),
  helper.accessor((node) => parseTime(node.lastSeen)?.getTime() ?? 0, {
    id: "lastSeen",
    header: "Last seen",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        {row.original.online ? "Now" : <RelativeTime value={row.original.lastSeen} />}
      </span>
    ),
    meta: { className: "hidden whitespace-nowrap lg:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, users } = table.options.meta ?? {};

      return me === undefined ? null : (
        <MachineMenu node={row.original} me={me} users={users ?? emptyUsers} />
      );
    },
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ node }: { readonly node: Node }): ReactElement {
  const name = nodeName(node);

  return (
    <div className="flex min-w-0 flex-col gap-1">
      <Link
        to="/machines/$nodeId"
        params={{ nodeId: node.id }}
        className="block truncate font-medium text-kumo-default hover:underline"
      >
        {name}
      </Link>
      <div className="flex flex-wrap items-center gap-1.5 text-sm text-kumo-subtle">
        {node.name === name ? null : (
          <span className="truncate font-mono text-[0.9em]">{node.name}</span>
        )}
        <Attributes node={node} />
      </div>
    </div>
  );
}

function Attributes({ node }: { readonly node: Node }): ReactElement | null {
  const subnets = approvedSubnets(node);
  const pending = pendingRoutes(node);
  const items: ReactElement[] = [];

  if (node.globalExitNode) {
    items.push(
      <Tooltip key="global" content="Global exit node: every client is told to prefer it">
        <Badge variant="blue" icon={StarIcon}>
          Global exit
        </Badge>
      </Tooltip>,
    );
  } else if (isExitNode(node)) {
    items.push(
      <Tooltip key="exit" content="Exit node">
        <Badge variant="outline" icon={GlobeIcon}>
          Exit node
        </Badge>
      </Tooltip>,
    );
  }

  if (subnets.length > 0) {
    items.push(
      <Tooltip key="subnets" content={subnets.join(", ")}>
        <Badge variant="outline" icon={PathIcon}>
          {subnets.length === 1 ? "1 subnet" : `${subnets.length} subnets`}
        </Badge>
      </Tooltip>,
    );
  }

  if (pending.length > 0) {
    items.push(
      <Tooltip key="pending" content={`Waiting for approval: ${pending.join(", ")}`}>
        <Badge variant="warning" icon={PathIcon}>
          {pending.length === 1 ? "1 route pending" : `${pending.length} routes pending`}
        </Badge>
      </Tooltip>,
    );
  }

  if (node.sharedWith.length > 0) {
    items.push(
      <Tooltip key="shared" content="Shared with other users">
        <Badge variant="outline" icon={ShareNetworkIcon}>
          Shared
        </Badge>
      </Tooltip>,
    );
  }

  return items.length === 0 ? null : <span className="flex flex-wrap gap-1">{items}</span>;
}

function OwnerCell({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return (
      <div className="flex flex-wrap gap-1">
        {node.tags.map((tag) => (
          <Badge key={tag} variant="neutral">
            <span className="font-mono text-[0.9em]">{tag}</span>
          </Badge>
        ))}
      </div>
    );
  }

  return (
    <Link to="/users" search={{ q: node.user.name }} className="text-kumo-default hover:underline">
      {ownerLabel(node)}
    </Link>
  );
}

function AddressCell({ node }: { readonly node: Node }): ReactElement {
  return (
    <div className="flex flex-col font-mono text-[0.9em] text-kumo-subtle">
      {node.ipAddresses.map((address) => (
        <span key={address}>{address}</span>
      ))}
    </div>
  );
}

function StatusCell({ node }: { readonly node: Node }): ReactElement {
  const status = nodeStatus(node);
  const expiry = parseTime(node.expiry);

  return (
    <div className="flex flex-col items-start gap-1">
      <StatusBadge status={status} />
      {expiry === null ? (
        <span className="text-sm text-kumo-subtle">Key never expires</span>
      ) : (
        <span className="text-sm text-kumo-subtle">
          {status === "expired" ? "Expired " : "Expires "}
          <RelativeTime value={node.expiry} />
        </span>
      )}
    </div>
  );
}
