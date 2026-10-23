import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { PlusIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import type { ReactElement } from "react";

import { accessRequestOptionsQuery, myAccessRequestsQuery } from "~/api/queries.ts";
import { askReason } from "~/components/access/request-access-page.tsx";
import { requestColumns } from "~/components/access/request-columns.tsx";
import { toRequestRows } from "~/components/access/request-model.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableGroup } from "~/components/table/toolbar.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

export const Route = createFileRoute("/_app/access")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(myAccessRequestsQuery),
      context.queryClient.query(accessRequestOptionsQuery),
    ]);
  },
  component: MyAccessPage,
});

/** Why the list is empty, and what would fill it. */
function emptyText(hasUser: boolean, hasGroups: boolean): string {
  if (!hasUser) {
    return "Sign in as a user to ask for access.";
  }

  if (!hasGroups) {
    return "No group takes requests. An administrator can mark one requestable under Access controls.";
  }

  return "Your requests and their outcomes show here.";
}

/** The signed-in user's own asks for temporary access, and the way to file one. */
function MyAccessPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { requests } = useSuspenseQuery(myAccessRequestsQuery).data;
  const options = useSuspenseQuery(accessRequestOptionsQuery).data;
  const rows = useMemo(
    () =>
      toRequestRows(requests, {
        groups: options.groups,
        users: me.user === undefined ? [] : [me.user],
        nodes: options.nodes,
      }),
    [requests, options, me.user],
  );

  const table = useAppTable({
    data: rows,
    columns: requestColumns,
    getRowId: (request) => request.id,
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, canDecide: false },
  });

  const canAsk = me.user !== undefined && options.groups.length > 0;
  const active = rows.filter((row) => row.phase === "active").length;

  return (
    <>
      <PageHeader
        title="My access"
        docs="ref/temporary-access"
        description="Ask to join a group for a set time. An approver decides, and access ends when the time is up."
        meta={active === 0 ? undefined : `${active} active ${active === 1 ? "grant" : "grants"}`}
        actions={
          <DisabledReason reason={askReason(me.user !== undefined, options.groups.length > 0)}>
            {canAsk ? (
              <LinkButton href="/access/request" variant="primary" icon={PlusIcon}>
                Request access
              </LinkButton>
            ) : (
              <Button variant="primary" icon={PlusIcon} disabled>
                Request access
              </Button>
            )}
          </DisabledReason>
        }
      />
      <TableGroup>
        <DataTable
          table={table}
          empty={
            <SectionEmpty
              title="No requests"
              description={emptyText(me.user !== undefined, options.groups.length > 0)}
            />
          }
        />
      </TableGroup>
    </>
  );
}
