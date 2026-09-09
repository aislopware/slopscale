import type { AccessRequest, Group, Node, User } from "~/api/queries.ts";
import { groupName } from "~/components/access/model.ts";
import { nodeName, userLabel } from "~/lib/node.ts";
import { daySeconds, hourSeconds, isPast, minuteSeconds, parseTime } from "~/lib/time.ts";

export type RequestStatus = AccessRequest["status"];

/** How a request reads once its outcome and, for an approval, its expiry are known. */
export type RequestPhase = "pending" | "active" | "expired" | "denied" | "cancelled";

export function requestPhase(request: AccessRequest, now: Date = new Date()): RequestPhase {
  switch (request.status) {
    case "pending": {
      return "pending";
    }
    case "approved": {
      return isPast(parseTime(request.expiresAt), now) ? "expired" : "active";
    }
    case "denied": {
      return "denied";
    }
    default: {
      return "cancelled";
    }
  }
}

export const phaseLabels: Record<RequestPhase, string> = {
  pending: "Pending",
  active: "Active",
  expired: "Expired",
  denied: "Denied",
  cancelled: "Withdrawn",
};

const halfHourMinutes = 30;
const shiftHours = 4;
const workdayHours = 8;
const weekDays = 7;
const monthDays = 30;
const halfHour = halfHourMinutes * minuteSeconds;
const fourHours = shiftHours * hourSeconds;
const eightHours = workdayHours * hourSeconds;
const week = weekDays * daySeconds;
const month = monthDays * daySeconds;

/** The durations the request dialog offers, in seconds. */
export const durationChoices: readonly { seconds: number; label: string }[] = [
  { seconds: halfHour, label: "30 minutes" },
  { seconds: hourSeconds, label: "1 hour" },
  { seconds: fourHours, label: "4 hours" },
  { seconds: eightHours, label: "8 hours" },
  { seconds: daySeconds, label: "1 day" },
  { seconds: week, label: "7 days" },
  { seconds: month, label: "30 days" },
];

/** A request with the names the table shows, so the global filter can match them. */
export interface RequestRow extends AccessRequest {
  readonly userName: string;
  readonly nodeLabel: string;
  readonly groupLabel: string;
  readonly phase: RequestPhase;
}

/** The records a request names, as the caller may list them; a missing one shows as its id. */
export interface RequestNames {
  readonly groups: readonly { id: string; name: string }[];
  readonly users: readonly { id: string; name: string; displayName: string }[];
  readonly nodes: readonly { id: string; name: string }[];
}

export function requestNames(
  groups: readonly Group[],
  users: readonly User[] | undefined,
  nodes: readonly Node[] | undefined,
): RequestNames {
  return {
    groups,
    users: users ?? [],
    nodes: (nodes ?? []).map((node) => ({ id: node.id, name: nodeName(node) })),
  };
}

export function toRequestRows(
  requests: readonly AccessRequest[],
  names: RequestNames,
): RequestRow[] {
  return requests.map((request) => {
    const user = names.users.find((candidate) => candidate.id === request.userId);
    const node = names.nodes.find((candidate) => candidate.id === request.nodeId);

    return {
      ...request,
      userName: user === undefined ? `User ${request.userId}` : userLabel(user),
      nodeLabel: nodeLabelFor(request.nodeId, node),
      groupLabel: groupName(names.groups, request.groupId),
      phase: requestPhase(request),
    };
  });
}

function nodeLabelFor(nodeId: string | undefined, node: { name: string } | undefined): string {
  if (nodeId === undefined) {
    return "Every machine you own";
  }

  return node === undefined ? `Machine ${nodeId}` : node.name;
}

export function pendingCount(requests: readonly AccessRequest[]): number {
  return requests.filter((request) => request.status === "pending").length;
}
