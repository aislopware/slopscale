import type { Node } from "~/api/queries.ts";
import { isPast, parseTime } from "~/lib/time.ts";

export const exitRoutes: readonly string[] = ["0.0.0.0/0", "::/0"];

export function isExitRoute(route: string): boolean {
  return exitRoutes.includes(route);
}

export type NodeStatus = "online" | "offline" | "pending" | "expired";

export function nodeStatus(node: Node, now: Date = new Date()): NodeStatus {
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
