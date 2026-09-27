import type { Node } from "~/api/queries.ts";
import type { MachineOwner } from "~/api/schema.gen.ts";
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
  return machineOwnerLabel(nodeOwner(node));
}

/**
 * Whose a machine is, in the shape the API sends beside every machine name. A tagged node's `user`
 * is the synthetic "Tagged Devices" one, so the tags stand in for it.
 */
export function nodeOwner(node: Node): MachineOwner {
  if (isTagged(node)) {
    return { tags: node.tags, userId: "", userName: "", displayName: "", profilePicUrl: "" };
  }

  return {
    tags: [],
    userId: node.user.id,
    userName: node.user.name,
    displayName: node.user.displayName,
    profilePicUrl: node.user.profilePicUrl,
  };
}

/** The tags, or the person; empty when the machine is gone and nobody knows. */
export function machineOwnerLabel(owner: MachineOwner | undefined): string {
  if (owner === undefined) {
    return "";
  }

  if (owner.tags.length > 0) {
    return owner.tags.join(", ");
  }

  return owner.displayName === "" ? owner.userName : owner.displayName;
}

/**
 * A machine as one line of text: "localhost (Alice Nguyen)". A given name such as "localhost" says
 * neither which machine nor whose, so it is never shown on its own where there is room for only one
 * line: a select, a breadcrumb, a sentence, an aria-label.
 */
export function machineLabel(name: string, owner: MachineOwner | undefined): string {
  const whose = machineOwnerLabel(owner);

  return whose === "" ? name : `${name} (${whose})`;
}

export function nodeLabel(node: Node): string {
  return machineLabel(nodeName(node), nodeOwner(node));
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
