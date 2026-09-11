import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { roleName, toRole } from "~/components/users/roles.ts";
import type { UserRole } from "~/components/users/roles.ts";
import { hueColours } from "~/lib/hue.ts";

const sizes = {
  /** Beside a name in a menu or a meta line. */
  sm: "h-5 px-1.5 text-xs",
  /** In a table cell or a definition list. */
  base: "h-6 px-2 text-sm",
} as const;

/**
 * One fixed hue per role, far enough apart on the circle to tell apart at a glance and ordered by
 * warmth: the owner in the brand's orange, the admins in purple and blue, the reading roles in
 * green and yellow. A member is the ordinary case and takes the recessed surface, so the tinted
 * chips are the people with a say.
 */
const roleHues: Record<Exclude<UserRole, "member">, number> = {
  owner: 45,
  admin: 300,
  "network-admin": 240,
  "it-admin": 165,
  auditor: 95,
};

/**
 * A user's role as a chip in the role's own tint, the same tint on every page, so a list of users
 * shows who holds a role before the words are read. It goes wherever a role is stated: the users
 * and invitations tables, the account menu, the overview's signed-in line, the session page.
 */
export function RoleBadge({
  role,
  size = "base",
  className,
}: {
  readonly role: string;
  readonly size?: keyof typeof sizes;
  readonly className?: string;
}): ReactElement {
  const known = toRole(role);

  return (
    <span
      data-role={known}
      className={cn(
        "inline-flex shrink-0 items-center rounded-md leading-none font-medium whitespace-nowrap ring ring-kumo-line ring-inset",
        known === "member" && "bg-kumo-recessed text-kumo-subtle",
        sizes[size],
        className,
      )}
      style={known === "member" ? undefined : hueColours(roleHues[known])}
    >
      {roleName(known)}
    </span>
  );
}
