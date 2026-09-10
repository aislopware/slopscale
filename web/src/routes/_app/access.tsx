import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import type { ReactElement } from "react";

import { accessRequestOptionsQuery, myAccessRequestsQuery } from "~/api/queries.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { RequestAccessDialog } from "~/components/access/request-access-dialog.tsx";
import { requestColumns } from "~/components/access/request-columns.tsx";
import { toRequestRows } from "~/components/access/request-model.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/access")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(myAccessRequestsQuery),
      context.queryClient.query(accessRequestOptionsQuery),
    ]);
  },
  component: MyAccessPage,
});

/** Why asking is not possible, or nothing when it is. */
function askReason(hasUser: boolean, hasGroups: boolean): string | undefined {
  if (!hasUser) {
    return "These credentials are not a user";
  }

  return hasGroups ? undefined : "No group takes requests right now";
}

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
  const [requesting, setRequesting] = useState(false);
  const mutations = useAccessMutations();
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
        description="Ask to join a group for a set time. An approver decides, and access ends when the time is up."
        meta={active === 0 ? undefined : `${active} active ${active === 1 ? "grant" : "grants"}`}
        actions={
          <DisabledReason reason={askReason(me.user !== undefined, options.groups.length > 0)}>
            <Button
              variant="primary"
              icon={PlusIcon}
              disabled={!canAsk}
              onClick={() => {
                setRequesting(true);
              }}
            >
              Request access
            </Button>
          </DisabledReason>
        }
      />
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No requests"
                description={emptyText(me.user !== undefined, options.groups.length > 0)}
                contents={
                  canAsk ? (
                    <Button
                      variant="secondary"
                      onClick={() => {
                        setRequesting(true);
                      }}
                    >
                      Request access
                    </Button>
                  ) : undefined
                }
              />
            }
            footer={
              rows.length === 0 ? undefined : (
                <TableFooter>
                  {rows.length === 1 ? "1 request" : `${rows.length} requests`}
                </TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <RequestAccessDialog
        options={options}
        open={requesting}
        onOpenChange={setRequesting}
        mutations={mutations}
      />
    </>
  );
}
