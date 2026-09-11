import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import {
  FunnelIcon,
  GlobeIcon,
  HardDrivesIcon,
  HourglassIcon,
  PathIcon,
  ShareNetworkIcon,
  StarIcon,
  TagIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { expiryWorthShowing } from "~/components/machines/filters.ts";
import { MachineMenu } from "~/components/machines/menu.tsx";
import { SelectAllCheckbox, SelectRowCheckbox } from "~/components/machines/selection.tsx";
import { StatusBadge } from "~/components/machines/status-badge.tsx";
import { hostedServices } from "~/components/services/model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { Flagged } from "~/components/ui/flagged.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
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

const statusOrder = { online: 0, pending: 1, suspended: 2, offline: 3, expired: 4 } as const;
const markSize = 13;

/**
 * The tick box that puts a machine into a bulk action. It is a column of its own rather than part
 * of the name cell, so the header can carry the "select everything" box.
 */
const selectColumn = helper.display({
  id: "select",
  header: () => <SelectAllCheckbox />,
  cell: ({ row }) => <SelectRowCheckbox id={row.original.id} name={nodeName(row.original)} />,
  meta: { className: "w-10" },
});

export const columns = helper.columns([
  helper.accessor((node) => nodeName(node), {
    id: "name",
    header: "Machine",
    enableSorting: true,
    cell: ({ row }) => <NameCell node={row.original} />,
    meta: { className: "w-[30%] min-w-56" },
  }),
  helper.accessor((node) => ownerLabel(node), {
    id: "owner",
    header: "Owner",
    enableSorting: true,
    cell: ({ row }) => <OwnerCell node={row.original} />,
    meta: { className: "w-[18%]" },
  }),
  // The hostname rides along here so a search matches it without a column of its own.
  helper.accessor((node) => [...node.ipAddresses, node.name].join(" "), {
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
    cell: ({ row }) => <LastSeenCell node={row.original} />,
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
    meta: { className: "w-12 text-right", sticky: "right" },
  }),
]);

/** The same table with the bulk-action tick boxes, for a caller that may act on a machine. */
export const selectableColumns = helper.columns([selectColumn, ...columns]);

function NameCell({ node }: { readonly node: Node }): ReactElement {
  const name = nodeName(node);

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <Link
        to="/machines/$nodeId"
        params={{ nodeId: node.id }}
        className="block truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
      >
        {name}
      </Link>
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-kumo-subtle">
        {node.name === name ? null : (
          <span className="truncate font-mono text-xs">{node.name}</span>
        )}
        {node.tags.length === 0 ? null : <TagList tags={node.tags} size="sm" />}
        <Attributes node={node} />
      </div>
    </div>
  );
}

/** What the machine does for the tailnet, as icons; the actionable one keeps its words. */
function Attributes({ node }: { readonly node: Node }): ReactElement | null {
  const subnets = approvedSubnets(node);
  const pending = pendingRoutes(node);
  const marks: ReactElement[] = [];

  if (node.globalExitNode) {
    marks.push(
      <Mark key="global" hint="Global exit node, preferred by every client">
        <StarIcon size={markSize} weight="fill" className="text-kumo-warning" />
      </Mark>,
    );
  } else if (isExitNode(node)) {
    marks.push(
      <Mark key="exit" hint="Approved exit node">
        <GlobeIcon size={markSize} />
      </Mark>,
    );
  }

  if (subnets.length > 0) {
    marks.push(
      <Mark key="subnets" hint={`Routes ${subnets.join(", ")}`}>
        <PathIcon size={markSize} />
      </Mark>,
    );
  }

  if (node.funnelEnabled) {
    marks.push(
      <Mark
        key="funnel"
        hint="Funnel is on: a service on this machine is reachable from the internet"
      >
        <FunnelIcon size={markSize} />
      </Mark>,
    );
  }

  const hosted = hostedServices(node);

  if (hosted.length > 0) {
    marks.push(
      <Mark key="services" hint={`Hosts ${hosted.join(", ")}`}>
        <HardDrivesIcon size={markSize} />
      </Mark>,
    );
  }

  if (node.sharedWith.length > 0) {
    marks.push(
      <Mark key="shared" hint="Shared with other users">
        <ShareNetworkIcon size={markSize} />
      </Mark>,
    );
  }

  if (node.ephemeral) {
    marks.push(
      <Mark key="ephemeral" hint="Ephemeral, deleted when it logs out or goes offline">
        <HourglassIcon size={markSize} />
      </Mark>,
    );
  }

  if (marks.length === 0 && pending.length === 0) {
    return null;
  }

  return (
    <span className="flex items-center gap-1.5">
      {marks}
      {pending.length === 0 ? null : (
        <Flagged
          icon={PathIcon}
          title="Waiting for approval"
          detail={<Code className="whitespace-normal">{pending.join(", ")}</Code>}
        >
          {pending.length === 1 ? "1 route" : `${pending.length} routes`}
        </Flagged>
      )}
    </span>
  );
}

function Mark({
  hint,
  children,
}: {
  readonly hint: string;
  readonly children: ReactElement;
}): ReactElement {
  // The trigger is a Kumo button so it has a name for the screen reader and a focus ring for
  // the keyboard; the icon is decoration.
  return (
    <Tooltip
      content={hint}
      render={
        <Button
          variant="ghost"
          shape="square"
          size="xs"
          aria-label={hint}
          className="h-lh text-kumo-subtle"
          icon={<span aria-hidden>{children}</span>}
        />
      }
    />
  );
}

function OwnerCell({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return (
      <span className="flex items-center gap-1.5 text-kumo-subtle">
        <span className="flex h-lh items-center">
          <TagIcon size={markSize} />
        </span>
        Tagged
      </span>
    );
  }

  const label = ownerLabel(node);

  return (
    // A table cell grows to its content, so the cap is what lets a long name truncate.
    <span className="flex max-w-36 min-w-0 items-center gap-2" title={label}>
      <Avatar name={label} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}

function AddressCell({ node }: { readonly node: Node }): ReactElement {
  return (
    <div className="flex flex-col items-start gap-0.5 whitespace-nowrap text-kumo-subtle">
      {node.ipAddresses.map((address) => (
        <CopyText key={address} value={address} />
      ))}
    </div>
  );
}

function StatusCell({ node }: { readonly node: Node }): ReactElement {
  const status = nodeStatus(node);
  const soon = expiryWorthShowing(parseTime(node.expiry));

  return (
    <div className="flex flex-col items-start gap-1">
      <StatusBadge status={status} />
      {soon ? (
        <span className="text-xs text-kumo-subtle">
          {status === "expired" ? "Expired " : "Expires "}
          <RelativeTime value={node.expiry} />
        </span>
      ) : null}
    </div>
  );
}

function LastSeenCell({ node }: { readonly node: Node }): ReactElement {
  return (
    <span className="text-kumo-subtle">
      {node.online ? "Now" : <RelativeTime value={node.lastSeen} />}
    </span>
  );
}
