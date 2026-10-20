import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import { roleName, toRole } from "~/components/users/roles.ts";

/**
 * A user's role as Kumo's Badge, the dashboard's pill: outlined for a role with a say (owner, the
 * admins, the auditor), the neutral secondary fill for an ordinary member. Roles are words, not
 * colours, so every role badge is the same ink. It goes wherever a role is stated: the users and
 * invitations tables, the overview's signed-in line, the session page.
 */
export function RoleBadge({
  role,
  className,
}: {
  readonly role: string;
  /** Kept for callers; a Kumo badge has one size. */
  readonly size?: "sm" | "base";
  readonly className?: string;
}): ReactElement {
  const known = toRole(role);

  return (
    <span data-role={known} className="inline-flex shrink-0">
      <Badge
        variant={known === "member" ? "secondary" : "outline"}
        {...(className === undefined ? {} : { className })}
      >
        {roleName(known)}
      </Badge>
    </span>
  );
}
