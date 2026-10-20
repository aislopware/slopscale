import { Button } from "@cloudflare/kumo/components/button";
import { Popover } from "@cloudflare/kumo/components/popover";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import {
  FunnelIcon,
  GlobeIcon,
  HardDrivesIcon,
  HourglassIcon,
  PathIcon,
  ShareNetworkIcon,
  StarIcon,
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
import { cellSecondaryClass, narrowOnlyClass, rowLinkClass } from "~/components/table/styles.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { Flagged } from "~/components/ui/flagged.tsx";
import { OsMark } from "~/components/ui/os-mark.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import {
  approvedSubnets,
  isExitNode,
  isTagged,
  nodeLabel,
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
const popoverDelay = 150;
/** Tags an owner cell shows before it counts the rest; the column is 18% of the table. */
const maxOwnerTags = 2;

/**
 * The tick box that puts a machine into a bulk action. It is a column of its own rather than part
 * of the name cell, so the header can carry the "select everything" box.
 */
const selectColumn = helper.display({
  id: "select",
  header: () => <SelectAllCheckbox />,
  cell: ({ row }) => <SelectRowCheckbox id={row.original.id} name={nodeLabel(row.original)} />,
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
    meta: { hideBelow: "sm" },
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

/**
 * The machine on one line, so a row keeps the dashboard's 44px: the OS mark, the name as the row's
 * link and what the machine does for the tailnet as icons after it. The hostname gets a second line
 * only where it differs from the name, the two-line cell the dashboard uses for a name and its
 * subtitle.
 */
function NameCell({ node }: { readonly node: Node }): ReactElement {
  const name = nodeName(node);

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="flex min-w-0 items-center gap-1.5">
        <OsMark os={node.os} version={node.osVersion} />
        <Link
          to="/machines/$nodeId"
          params={{ nodeId: node.id }}
          className={cn("block min-w-0 truncate font-medium", rowLinkClass)}
        >
          {name}
        </Link>
        <Attributes node={node} />
      </span>
      {node.name === name ? null : (
        <span className={cn(cellSecondaryClass, "font-mono")}>{node.name}</span>
      )}
      {/* A phone hides the Status column, so the state takes a line of its own. */}
      <span className={cn("pt-0.5", narrowOnlyClass)}>
        <StatusCell node={node} />
      </span>
    </div>
  );
}

/** What the machine does for the tailnet, as icons; the actionable one keeps its words. */
function Attributes({ node }: { readonly node: Node }): ReactElement | null {
  const subnets = approvedSubnets(node);
  const pending = pendingRoutes(node);
  const marks: ReactElement[] = [];

  if (node.globalExitNode) {
    const hint =
      node.exitNodePriority > 0
        ? `Global exit node, priority ${node.exitNodePriority}`
        : "Global exit node, preferred by every client";

    marks.push(
      <Mark key="global" hint={hint}>
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
    <span className="flex shrink-0 items-center gap-1 text-xs text-kumo-subtle">
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

/**
 * Who the machine belongs to: the person, or the tags themselves. A tagged machine used to read
 * "Tagged" here beside the chips that already said which tags, so the column repeated the row.
 */
function OwnerCell({ node }: { readonly node: Node }): ReactElement {
  if (isTagged(node)) {
    return <TagList tags={node.tags} size="sm" max={maxOwnerTags} />;
  }

  const label = ownerLabel(node);

  return (
    // A table cell grows to its content, so the cap is what lets a long name truncate.
    <span className="flex max-w-36 min-w-0 items-center gap-2" title={label}>
      <Avatar name={label} id={node.user.id} src={node.user.profilePicUrl} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}

/**
 * The machine's IPv4 address, click to copy, with the others one hover away: two addresses stacked
 * made every row two lines tall, and the IPv6 one is the address an operator types least.
 */
function AddressCell({ node }: { readonly node: Node }): ReactElement {
  const [first, ...rest] = node.ipAddresses.toSorted(
    (left, right) => Number(left.includes(":")) - Number(right.includes(":")),
  );

  if (first === undefined) {
    return <span className="text-kumo-subtle">—</span>;
  }

  return (
    <div className="flex items-center gap-1.5 whitespace-nowrap text-kumo-subtle">
      <CopyText value={first} />
      {rest.length === 0 ? null : <OtherAddresses name={nodeName(node)} addresses={rest} />}
    </div>
  );
}

/** "IPv6" (or "+2") beside the first address, opening the rest, each one click to copy. */
function OtherAddresses({
  name,
  addresses,
}: {
  readonly name: string;
  readonly addresses: readonly string[];
}): ReactElement {
  const label =
    addresses.length === 1 && addresses[0]?.includes(":") === true
      ? "IPv6"
      : `+${addresses.length}`;

  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={popoverDelay}
        aria-label={`Other addresses of ${name}`}
        className="inline-flex h-5 items-center rounded-full px-1.5 text-xs text-kumo-subtle ring ring-kumo-line outline-none hover:text-kumo-default focus-visible:ring-2 focus-visible:ring-kumo-focus"
      >
        {label}
      </Popover.Trigger>
      <Popover.Content side="top" className="gap-1">
        {addresses.map((address) => (
          <CopyText key={address} value={address} className="text-kumo-default" />
        ))}
      </Popover.Content>
    </Popover>
  );
}

function StatusCell({ node }: { readonly node: Node }): ReactElement {
  const status = nodeStatus(node);
  const soon = expiryWorthShowing(parseTime(node.expiry));

  return (
    <div className="flex items-center gap-2 whitespace-nowrap">
      <StatusBadge status={status} />
      {/* The badge already says the key expired; the words beside it only say when. */}
      {soon ? (
        <span className="text-xs text-kumo-subtle">
          {status === "expired" ? null : "Expires "}
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
