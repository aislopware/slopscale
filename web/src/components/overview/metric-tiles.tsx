import { SkeletonLine } from "@cloudflare/kumo/components/loader";
import { cn } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { Node, User } from "~/api/queries.ts";
import { allUsers } from "~/components/overview/links.ts";
import { plural } from "~/components/overview/plural.ts";
import { Frame, framePanelClass } from "~/components/ui/frame.tsx";
import { isExitNode, nodeName } from "~/lib/node.ts";

/** Column count by tile count, so a shorter strip still fills its row. */
const columnsFor: readonly string[] = [
  "grid-cols-1",
  "grid-cols-1",
  "grid-cols-1 sm:grid-cols-2",
  "grid-cols-1 lg:grid-cols-3",
  "grid-cols-1 sm:grid-cols-2 lg:grid-cols-4",
];

/** Each tile is its own inset panel in the strip's Frame. */
const tileClass = cn(
  framePanelClass,
  "flex flex-col gap-1.5 px-5 py-4 text-kumo-default no-underline hover:bg-kumo-tint focus-visible:bg-kumo-tint",
);

type Tone = "neutral" | "warning";

const percent = 100;

/**
 * How much of the number the context line speaks for, as a line under it: a recessed track and an
 * ink fill, so "8 connected" of 9 is seen before it is read. Nothing is drawn for a number that is
 * not a share of anything.
 */
function Meter({ share, of }: { readonly share: number; readonly of: number }): ReactElement {
  const width = of === 0 ? 0 : Math.round((share / of) * percent);

  return (
    <span
      aria-hidden
      className="mt-1 block h-1 w-full overflow-hidden rounded-full bg-kumo-recessed"
    >
      <span className="block h-full rounded-full bg-kumo-contrast" style={{ width: `${width}%` }} />
    </span>
  );
}

/** A label, the number and one line of context; no icon, the words say what the number counts. */
function TileBody({
  label,
  value,
  context,
  tone = "neutral",
  share,
}: {
  readonly label: string;
  readonly value: number;
  readonly context: ReactNode;
  readonly tone?: Tone;
  /** How much of the number the context counts, for a meter under it. */
  readonly share?: number;
}): ReactElement {
  return (
    <>
      <span className="truncate text-kumo-subtle">{label}</span>
      <span
        className={cn(
          "text-2xl font-semibold tabular-nums",
          tone === "warning" ? "text-kumo-warning" : "text-kumo-strong",
        )}
      >
        {value}
      </span>
      <span className="truncate text-xs text-kumo-subtle">{context}</span>
      {share === undefined ? null : <Meter share={share} of={value} />}
    </>
  );
}

function MachinesTile({ nodes }: { readonly nodes: readonly Node[] }): ReactElement {
  const online = nodes.filter((node) => node.online).length;

  return (
    <Link to="/machines" className={tileClass}>
      <TileBody
        label="Machines"
        value={nodes.length}
        context={online === 0 ? "None connected" : `${online} connected`}
        share={online}
      />
    </Link>
  );
}

function ApprovalTile({
  nodes,
  pendingUsers,
}: {
  readonly nodes: readonly Node[];
  readonly pendingUsers: number;
}): ReactElement {
  const pendingNodes = nodes.filter((node) => !node.approved).length;
  const pending = pendingNodes + pendingUsers;

  return (
    <Link to="/machines" search={{ status: "pending" }} className={tileClass}>
      <TileBody
        label="Needs approval"
        value={pending}
        context={pending === 0 ? "Nothing waiting" : waitingContext(pendingNodes, pendingUsers)}
        tone={pending === 0 ? "neutral" : "warning"}
      />
    </Link>
  );
}

function waitingContext(nodes: number, users: number): string {
  if (users === 0) {
    return plural(nodes, "machine");
  }

  return nodes === 0
    ? plural(users, "user")
    : `${plural(nodes, "machine")}, ${plural(users, "user")}`;
}

function UsersTile({
  users,
  pending,
}: {
  readonly users: readonly User[];
  readonly pending: number;
}): ReactElement {
  return (
    <Link to="/users" search={allUsers} className={tileClass}>
      <TileBody
        label="Users"
        value={users.length}
        context={pending === 0 ? "All approved" : `${pending} waiting`}
        share={users.length - pending}
      />
    </Link>
  );
}

function ExitTile({ nodes }: { readonly nodes: readonly Node[] }): ReactElement {
  const global = nodes.find((node) => node.globalExitNode);
  const count = nodes.filter((node) => isExitNode(node)).length;

  return (
    <Link to="/machines" className={tileClass}>
      <TileBody
        label="Exit nodes"
        value={count}
        context={global === undefined ? "No global exit node" : `${nodeName(global)} is global`}
      />
    </Link>
  );
}

function TileSkeleton(): ReactElement {
  return (
    <div aria-busy className={tileClass}>
      <SkeletonLine blockHeight={24} minWidth={35} maxWidth={55} />
      <SkeletonLine blockHeight={32} minWidth={20} maxWidth={30} />
      <SkeletonLine blockHeight={16} minWidth={40} maxWidth={70} />
    </div>
  );
}

export interface MetricTilesProps {
  readonly nodes: readonly Node[] | undefined;
  readonly users: readonly User[] | undefined;
  readonly nodesLoading: boolean;
  readonly usersLoading: boolean;
}

/**
 * The four numbers at the top of the overview. Every tile is a link to the page that owns it, and a
 * tile only appears once its data can be read, so a caller without a scope sees a shorter strip
 * rather than an empty box.
 */
export function MetricTiles({
  nodes,
  users,
  nodesLoading,
  usersLoading,
}: MetricTilesProps): ReactElement | null {
  const pendingUsers = users?.filter((user) => !user.approved).length ?? 0;
  const tiles: ReactElement[] = [];

  if (nodes === undefined) {
    if (nodesLoading) {
      tiles.push(<TileSkeleton key="machines" />, <TileSkeleton key="approval" />);
    }
  } else {
    tiles.push(
      <MachinesTile key="machines" nodes={nodes} />,
      <ApprovalTile key="approval" nodes={nodes} pendingUsers={pendingUsers} />,
    );
  }

  if (users === undefined) {
    if (usersLoading) {
      tiles.push(<TileSkeleton key="users" />);
    }
  } else {
    tiles.push(<UsersTile key="users" users={users} pending={pendingUsers} />);
  }

  if (nodes === undefined) {
    if (nodesLoading) {
      tiles.push(<TileSkeleton key="exits" />);
    }
  } else {
    tiles.push(<ExitTile key="exits" nodes={nodes} />);
  }

  if (tiles.length === 0) {
    return null;
  }

  return <Frame className={cn("grid gap-1", columnsFor[tiles.length])}>{tiles}</Frame>;
}
