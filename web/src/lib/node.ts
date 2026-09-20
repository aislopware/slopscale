import type { Node } from "~/api/queries.ts";
import { isPast, parseTime } from "~/lib/time.ts";

export const exitRoutes: readonly string[] = ["0.0.0.0/0", "::/0"];

export function isExitRoute(route: string): boolean {
  return exitRoutes.includes(route);
}

export type NodeStatus = "online" | "offline" | "pending" | "expired" | "suspended";

/** Suspension wins over the rest: a suspended machine is cut off whatever else is true of it. */
export function nodeStatus(node: Node, now: Date = new Date()): NodeStatus {
  if (node.suspended) {
    return "suspended";
  }

  if (!node.approved) {
    return "pending";
  }

  if (isPast(parseTime(node.expiry), now)) {
    return "expired";
  }

  return node.online ? "online" : "offline";
}

export function isTagged(node: Node): boolean {
  return node.tags.length > 0;
}

/** Whether the node advertises the default routes, approved or not. */
export function advertisesExit(node: Node): boolean {
  return node.availableRoutes.some(isExitRoute);
}

export function isExitNode(node: Node): boolean {
  return node.approvedRoutes.some(isExitRoute);
}

export function approvedSubnets(node: Node): readonly string[] {
  return node.approvedRoutes.filter((route) => !isExitRoute(route));
}

export function pendingRoutes(node: Node): readonly string[] {
  return node.availableRoutes.filter((route) => !node.approvedRoutes.includes(route));
}

/** The count of pending routes, with the exit pair (0.0.0.0/0 and ::/0) counted as one. */
export function pendingRouteCount(node: Node): number {
  const pending = pendingRoutes(node);
  const exit = pending.some((route) => isExitRoute(route));
  const subnets = pending.filter((route) => !isExitRoute(route)).length;

  return subnets + (exit ? 1 : 0);
}

/** The DNS label operators refer to the node by; hostname when it differs. */
export function nodeName(node: Node): string {
  return node.givenName === "" ? node.name : node.givenName;
}

export function ownerLabel(node: Node): string {
  if (isTagged(node)) {
    return node.tags.join(", ");
  }

  return node.user.displayName === "" ? node.user.name : node.user.displayName;
}

export function userLabel(user: { name: string; displayName: string }): string {
  return user.displayName === "" ? user.name : user.displayName;
}

/**
 * The identifiers under a user's name, each once: the username the policy refers to and the email
 * the operator knows them by. A provider that hands out no username sets both to the email address,
 * which read as the address twice over the display name.
 */
export function userAliases(user: {
  name: string;
  displayName: string;
  email: string;
}): readonly string[] {
  const label = userLabel(user);

  return [...new Set([user.name, user.email])].filter((alias) => alias !== "" && alias !== label);
}

/**
 * The one identifier that says who a name belongs to, where a second line is all there is: the
 * email, or the username when the name on the line is already the email.
 */
export function userHint(user: {
  name: string;
  displayName: string;
  email: string;
}): string | undefined {
  const aliases = userAliases(user);

  return aliases.find((alias) => alias === user.email) ?? aliases[0];
}
