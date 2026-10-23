import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { EnvelopeSimpleIcon, PlusIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { SectionEmpty } from "~/components/ui/section.tsx";

/**
 * The users list's create action, right of the search: a link to the add page; a disabled button
 * for a reader.
 */
export function AddUserButton({ me }: { readonly me: Me }): ReactElement {
  return can(me, "users") ? (
    <LinkButton href="/users/new" variant="primary" icon={PlusIcon}>
      Add user
    </LinkButton>
  ) : (
    <Button variant="primary" icon={PlusIcon} disabled>
      Add user
    </Button>
  );
}

/**
 * The users table's secondary action. "Add user" stays the page's one primary, because a local
 * account is what an operator reaches for when there is no identity provider to invite anyone
 * into.
 */
export function InviteUserButton({ me }: { readonly me: Me }): ReactElement {
  return can(me, "users") ? (
    <LinkButton href="/users/invite" variant="secondary" icon={EnvelopeSimpleIcon}>
      Invite
    </LinkButton>
  ) : (
    <Button variant="secondary" icon={EnvelopeSimpleIcon} disabled>
      Invite
    </Button>
  );
}

/**
 * There is not a single user yet. It says where users come from and stops there: the toolbar an
 * inch above holds "Add user", and a second copy of it inside the empty panel only made the page
 * ask twice.
 */
export function FirstUserEmpty(): ReactElement {
  return (
    <SectionEmpty
      title="No users yet"
      description="Users appear here after their first sign-in, or add one to hand out a pre-auth key."
    />
  );
}
