import type { Node } from "~/api/queries.ts";
import { isTagged } from "~/lib/node.ts";

/**
 * The user a machine belongs to, or null when it is tagged. Tags and users are exclusive, and the
 * server sends no user at all for a tagged machine, which the generated type does not admit: read
 * the owner through here rather than through `node.user` so a tagged machine cannot crash a page.
 */
export function ownerId(node: Node): string | null {
  return isTagged(node) ? null : node.user.id;
}
