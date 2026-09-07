import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { Node, User } from "~/api/queries.ts";
import { StatCard, StatSkeleton } from "~/components/overview/stat-card.tsx";
import type { StatTone } from "~/components/overview/stat-card.tsx";
import { isExitNode, nodeName } from "~/lib/node.ts";

interface Stat {
  readonly id: string;
  readonly label: string;
  readonly value: number;
  readonly sub: ReactNode;
  readonly tone: StatTone;
}

/** One placeholder per card the node list feeds, so the grid keeps its shape while it loads. */
const nodeSkeletons = ["machines", "approval", "exits"];

function machineStat(nodes: readonly Node[]): Stat {
  const online = nodes.filter((node) => node.online).length;

  return {
    id: "machines",
    label: "Machines",
    value: nodes.length,
    sub: `${online} connected`,
    tone: "neutral",
  };
}

function approvalStat(nodes: readonly Node[], pendingUsers: number): Stat {
  const pending = nodes.filter((node) => !node.approved).length + pendingUsers;

  return {
    id: "approval",
    label: "Needs approval",
    value: pending,
    sub:
      pending === 0 ? (
        "Nothing waiting"
      ) : (
        <Link
          to="/machines"
          search={{ status: "pending", q: "", user: "" }}
          className="text-kumo-link hover:underline"
        >
          Review
        </Link>
      ),
    tone: pending === 0 ? "neutral" : "warning",
  };
}

function userStat(users: readonly User[], pending: number): Stat {
  return {
    id: "users",
    label: "Users",
    value: users.length,
    sub: pending === 0 ? "All approved" : `${pending} waiting`,
    tone: "neutral",
  };
}

function exitStat(nodes: readonly Node[]): Stat {
  const global = nodes.find((node) => node.globalExitNode);

  return {
    id: "exits",
    label: "Exit nodes",
    value: nodes.filter((node) => isExitNode(node)).length,
    sub: global === undefined ? "No global exit node" : `Global: ${nodeName(global)}`,
    tone: "neutral",
  };
}

function buildStats(
  nodes: readonly Node[] | undefined,
  users: readonly User[] | undefined,
): Stat[] {
  const pendingUsers = users?.filter((user) => !user.approved).length ?? 0;
  const stats: Stat[] = [];

  if (nodes !== undefined) {
    stats.push(machineStat(nodes), approvalStat(nodes, pendingUsers));
  }

  if (users !== undefined) {
    stats.push(userStat(users, pendingUsers));
  }

  if (nodes !== undefined) {
    stats.push(exitStat(nodes));
  }

  return stats;
}

export interface StatsGridProps {
  readonly nodes: readonly Node[] | undefined;
  readonly users: readonly User[] | undefined;
  readonly nodesLoading: boolean;
  readonly usersLoading: boolean;
}

/** The numbers at the top of the overview; a card appears only once its data can be read. */
export function StatsGrid({
  nodes,
  users,
  nodesLoading,
  usersLoading,
}: StatsGridProps): ReactElement | null {
  const stats = buildStats(nodes, users);

  if (stats.length === 0 && !nodesLoading && !usersLoading) {
    return null;
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {nodesLoading ? nodeSkeletons.map((id) => <StatSkeleton key={id} />) : null}
      {usersLoading ? <StatSkeleton /> : null}
      {stats.map((stat) => (
        <StatCard
          key={stat.id}
          label={stat.label}
          value={stat.value}
          sub={stat.sub}
          tone={stat.tone}
        />
      ))}
    </div>
  );
}
