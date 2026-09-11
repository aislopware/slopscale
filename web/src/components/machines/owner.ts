import type { Node } from "~/api/queries.ts";
import { actsAsUser, can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { isTagged } from "~/lib/node.ts";

/**
 * The user a machine belongs to, or null when it is tagged. Tags and users are exclusive, and the
 * server sends no user at all for a tagged machine, which the generated type does not admit: read
 * the owner through here rather than through `node.user` so a tagged machine cannot crash a page.
 */
export function ownerId(node: Node): string | null {
  return isTagged(node) ? null : node.user.id;
}

/**
 * Whether the machine is one of the caller's own: a personal machine of the signed-in user. A
 * member looks after their own machines (rename, key expiry, remove, share) without any scope, and
 * the server holds them to the same line.
 */
export function ownsNode(me: Me, node: Node): boolean {
  return actsAsUser(me) && me.user !== undefined && ownerId(node) === me.user.id;
}

/** Whether the caller may rename, expire or remove the machine: the devices scope, or owning it. */
export function mayManageNode(me: Me, node: Node): boolean {
  return can(me, "devices:core") || ownsNode(me, node);
}
