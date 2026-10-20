import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { groupsQuery, invitesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { plural } from "~/components/overview/plural.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { inviteColumns } from "~/components/users/invite-columns.tsx";
import { openInvites } from "~/components/users/invites.ts";

/**
 * The invitations that have not been used yet, under the users table, as a layered card: the title
 * and the count on the grey band, the table resting in it. An accepted one is a user row above, so
 * only the open ones are listed; the whole card is absent for a reader who could not send one
 * anyway.
 */
export function PendingInvites({ me }: { readonly me: Me }): ReactElement | null {
  const readable = can(me, "users:read");
  const invites = useQuery({ ...invitesQuery, enabled: readable });
  const users = useQuery({ ...usersQuery, enabled: readable });
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const rows = openInvites(invites.data?.invites ?? []);

  const table = useAppTable({
    data: rows,
    columns: inviteColumns,
    getRowId: (invite) => invite.id,
    initialState: { sorting: [{ id: "expires", desc: false }] },
    meta: {
      me,
      ...(users.data === undefined ? {} : { users: users.data.users }),
      ...(groups.data === undefined ? {} : { groups: groups.data.groups }),
    },
  });

  if (!readable || (rows.length === 0 && !can(me, "users"))) {
    return null;
  }

  return (
    <Section
      title="Pending invitations"
      meta={rows.length === 0 ? undefined : plural(rows.length, "invitation")}
      description="An invitation becomes a user the first time its link is opened."
      panel={false}
    >
      <DataTable
        table={table}
        empty={
          <SectionEmpty
            title="No invitations waiting"
            description="An invitation stays here until it is used, revoked or expired."
          />
        }
      />
    </Section>
  );
}
