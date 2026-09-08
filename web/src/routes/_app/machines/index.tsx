import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, string } from "valibot";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { columns, emptyUsers } from "~/components/machines/columns.tsx";
import { MachinesEmpty } from "~/components/machines/empty.tsx";
import {
  defaultStatus,
  filterNodes,
  statusCounts,
  statusFilters,
  toStatusFilter,
} from "~/components/machines/filters.ts";
import { MachinesToolbar } from "~/components/machines/list-toolbar.tsx";
import { plural } from "~/components/overview/plural.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

/**
 * Every filter is optional and absent at its default, so the plain `/machines` link the sidebar and
 * the breadcrumbs point at is the URL the page produces when nothing is filtered.
 */
const optionalText = fallback(string(), "");
const optionalStatus = fallback(picklist(statusFilters), defaultStatus);

const searchSchema = object({
  q: optional(optionalText),
  status: optional(optionalStatus),
  user: optional(optionalText),
});

export const Route = createFileRoute("/_app/machines/")({
  validateSearch: searchSchema,
  loaderDeps: () => ({}),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(nodesQuery),
      can(context.me, "users:read") ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: MachinesPage,
});

function MachinesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const nodes = useSuspenseQuery(nodesQuery);
  const [addingMachine, setAddingMachine] = useState(false);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const status = toStatusFilter(search.status);
  const user = search.user ?? "";
  const query = search.q ?? "";
  const machines = nodes.data.nodes;
  const rows = filterNodes(machines, { status, user });
  const deferred = useDeferredValue(query);

  const table = useAppTable({
    data: rows,
    columns,
    getRowId: (node) => node.id,
    state: { globalFilter: deferred },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  const shown = table.getRowModel().rows.length;

  function clearFilters(): void {
    void navigate({ search: {} });
  }

  function addMachine(): void {
    setAddingMachine(true);
  }

  return (
    <>
      <PageHeader title="Machines" meta={summary(machines)} />
      <Frame>
        <MachinesToolbar
          me={me}
          query={query}
          onAddMachine={addMachine}
          status={status}
          user={user}
          users={users.data?.users}
          counts={statusCounts(machines)}
          onQueryChange={(value) => {
            void navigate({
              search: (previous) => ({ ...previous, q: value === "" ? undefined : value }),
              replace: true,
            });
          }}
          onStatusChange={(value) => {
            void navigate({
              search: (previous) => ({
                ...previous,
                status: value === defaultStatus ? undefined : value,
              }),
            });
          }}
          onUserChange={(value) => {
            void navigate({
              search: (previous) => ({ ...previous, user: value === "" ? undefined : value }),
            });
          }}
        />
        <table.AppTable>
          <DataTable
            onRowClick={(nodeId) => {
              void navigate({ to: "/machines/$nodeId", params: { nodeId } });
            }}
            empty={
              <MachinesEmpty
                total={machines.length}
                status={status}
                narrowed={query !== "" || user !== ""}
                canCreateKeys={can(me, "auth_keys")}
                onAddMachine={addMachine}
                onClearFilters={clearFilters}
              />
            }
            footer={
              machines.length === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${plural(machines.length, "machine")}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <CreatePreAuthKeyDialog
        me={me}
        intent="add-machine"
        open={addingMachine}
        onOpenChange={setAddingMachine}
      />
    </>
  );
}

/** The header's small facts line: how many machines there are and what is waiting. */
function summary(machines: readonly Node[]): ReactElement {
  const counts = statusCounts(machines);
  const total = machines.length === 1 ? "1 machine" : `${machines.length} machines`;

  return (
    <>
      <span>{total}</span>
      <span aria-hidden>·</span>
      <span>{counts.online} connected</span>
      {counts.pending === 0 ? null : (
        <>
          <span aria-hidden>·</span>
          <span className="text-kumo-warning">{counts.pending} waiting for approval</span>
        </>
      )}
    </>
  );
}
