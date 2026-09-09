import { Empty } from "@cloudflare/kumo/components/empty";
import { EnvelopeSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { groupsQuery, invitesQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { plural } from "~/components/overview/plural.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { Frame, FrameBand } from "~/components/ui/frame.tsx";
import { inviteColumns } from "~/components/users/invite-columns.tsx";
import { openInvites } from "~/components/users/invites.ts";

/**
 * The invitations that have not been used yet, under the users table. An accepted one is a user row
 * above, so only the open ones are listed; the whole panel is absent for a reader who could not
 * send one anyway.
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
    <Frame>
      {/* px-5 puts the title where the first column's text starts. */}
      <FrameBand className="px-5 pt-1.5 pb-2">
        <h2 className="font-semibold text-kumo-strong">Pending invitations</h2>
        <p className="text-kumo-subtle">
          An invitation becomes a user the first time its link is opened.
        </p>
      </FrameBand>
      <table.AppTable>
        <DataTable
          empty={
            <Empty
              className={tableEmptyClass}
              size="sm"
              icon={<EnvelopeSimpleIcon size={emptyIconSize} />}
              title="No invitations waiting"
              description="An invitation stays here until it is used, revoked or expired."
            />
          }
          footer={
            rows.length === 0 ? undefined : (
              <TableFooter>{`Showing ${plural(rows.length, "invitation")}`}</TableFooter>
            )
          }
        />
      </table.AppTable>
    </Frame>
  );
}
