import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, pipe, transform, unknown } from "valibot";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { MachineBulkBar } from "~/components/machines/bulk-bar.tsx";
import { columns, emptyUsers, selectableColumns } from "~/components/machines/columns.tsx";
import { MachinesEmpty } from "~/components/machines/empty.tsx";
import { FilterChips, machineChips } from "~/components/machines/filter-chips.tsx";
import {
  defaultStatus,
  filterNodes,
  filtersFromSearch,
  searchFromFilters,
  statusCounts,
  statusFilters,
  tagOptions,
} from "~/components/machines/filters.ts";
import type { MachineFilterState } from "~/components/machines/filters.ts";
import { MachinesToolbar } from "~/components/machines/list-toolbar.tsx";
import { machinePolling } from "~/components/machines/polling.ts";
import { SelectionProvider, useMachineSelection } from "~/components/machines/selection.tsx";
import { plural } from "~/components/overview/plural.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

/**
 * The router parses a search value as JSON, so `?user=2` arrives as the number 2 while the user id
 * it names is a string. Every text filter is read through this, which keeps a numeric id and drops
 * anything that is neither text nor a number.
 */
function toText(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }

  return typeof value === "number" ? String(value) : "";
}

/**
 * Every filter is optional and absent at its default, so the plain `/machines` link the sidebar and
 * the breadcrumbs point at is the URL the page produces when nothing is filtered.
 */
const optionalText = optional(pipe(unknown(), transform(toText)));
const optionalStatus = optional(fallback(picklist(statusFilters), defaultStatus));

const searchSchema = object({
  q: optionalText,
  status: optionalStatus,
  user: optionalText,
  tag: optionalText,
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
  const nodes = useSuspenseQuery({ ...nodesQuery, ...machinePolling });
  const [addingMachine, setAddingMachine] = useState(false);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const view = filtersFromSearch(search);
  const machines = nodes.data.nodes;
  const rows = filterNodes(machines, view);
  const deferred = useDeferredValue(view.query);
  const selection = useMachineSelection(rows.map((node) => node.id));
  const mayAct = can(me, "devices:core");

  const table = useAppTable({
    data: rows,
    columns: mayAct ? selectableColumns : columns,
    getRowId: (node) => node.id,
    state: { globalFilter: deferred },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  const shown = table.getRowModel().rows.length;

  // Typing narrows the list as the operator types, so a keystroke replaces the URL rather than
  // filling the back button with every prefix of the search.
  function setFilters(next: MachineFilterState, typed = false): void {
    void navigate({ search: () => searchFromFilters(next), replace: typed });
  }

  function clearFilters(): void {
    void navigate({ search: {} });
  }

  function addMachine(): void {
    setAddingMachine(true);
  }

  return (
    <>
      <PageHeader title="Machines" meta={summary(machines)} />
      <MachinesToolbar
        me={me}
        query={view.query}
        onAddMachine={addMachine}
        status={view.status}
        user={view.user}
        users={users.data?.users}
        tags={tagOptions(machines)}
        tag={view.tag}
        counts={statusCounts(machines)}
        onQueryChange={(value) => {
          setFilters({ ...view, query: value }, true);
        }}
        onStatusChange={(value) => {
          setFilters({ ...view, status: value });
        }}
        onUserChange={(value) => {
          setFilters({ ...view, user: value });
        }}
        onTagChange={(value) => {
          setFilters({ ...view, tag: value });
        }}
      />
      <Frame>
        <FilterChips
          chips={machineChips(view, users.data?.users, setFilters)}
          onClearAll={clearFilters}
        />
        <MachineBulkBar selection={selection} />
        <SelectionProvider selection={selection}>
          <table.AppTable>
            <DataTable
              onRowClick={(nodeId) => {
                void navigate({ to: "/machines/$nodeId", params: { nodeId } });
              }}
              empty={
                <MachinesEmpty
                  total={machines.length}
                  status={view.status}
                  narrowed={view.query !== "" || view.user !== "" || view.tag !== ""}
                  canCreateKeys={can(me, "auth_keys")}
                  onAddMachine={addMachine}
                  onClearFilters={clearFilters}
                />
              }
              footer={
                machines.length === 0 ? undefined : (
                  <TableFooter>
                    {`Showing ${shown} of ${plural(machines.length, "machine")}`}
                  </TableFooter>
                )
              }
            />
          </table.AppTable>
        </SelectionProvider>
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
