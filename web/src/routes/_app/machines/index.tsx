import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, pipe, transform, unknown } from "valibot";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MachineBulkBar } from "~/components/machines/bulk-bar.tsx";
import { columns, emptyUsers, selectableColumns } from "~/components/machines/columns.tsx";
import { MachinesEmpty, MachinesFirstRun } from "~/components/machines/empty.tsx";
import { FilterChips, machineChips } from "~/components/machines/filter-chips.tsx";
import {
  anyAttestation,
  attestationFilters,
  defaultAttestation,
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
import { TableFooter, TableGroup } from "~/components/table/toolbar.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { PageWithRail, RailList, RailRow, RailSection } from "~/components/ui/page.tsx";
import { docsUrl } from "~/lib/docs.ts";

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
const optionalAttestation = optional(fallback(picklist(attestationFilters), defaultAttestation));

const searchSchema = object({
  q: optionalText,
  status: optionalStatus,
  user: optionalText,
  tag: optionalText,
  attested: optionalAttestation,
});

export const Route = createFileRoute("/_app/machines/")({
  validateSearch: searchSchema,
  loaderDeps: () => ({}),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(nodesQuery),
      canListUsers(context.me) ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: MachinesPage,
});

/** What the page is, for a caller who sees every machine. */
const machinesNote =
  "Every device and server on the tailnet: who owns it, its tailnet addresses and whether it is connected. Approve, tag, share or remove one here.";

/** What a caller who sees only their own machines is looking at. */
const ownMachinesNote = "Your own machines, and the ones other users share with you.";

function MachinesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const nodes = useSuspenseQuery({ ...nodesQuery, ...machinePolling });

  // An empty tailnet gets Mesh's first-run page instead of an empty table: the picture, the card
  // that offers the first machine, and what a tailnet is for in the rail. A member who sees only
  // their own machines keeps the table, whose empty state says how theirs arrive.
  return nodes.data.nodes.length === 0 && can(me, "devices:core:read") ? (
    <FirstRunPage me={me} />
  ) : (
    <MachinesList />
  );
}

function FirstRunPage({ me }: { readonly me: Me }): ReactElement {
  const navigate = useNavigate({ from: Route.fullPath });

  return (
    <PageWithRail rail={<UseCases />}>
      <PageHeader title="Machines" docs="ref/device-management" description={machinesNote} />
      <MachinesFirstRun
        onAddMachine={
          can(me, "auth_keys")
            ? () => {
                void navigate({ to: "/machines/add" });
              }
            : undefined
        }
      />
    </PageWithRail>
  );
}

function MachinesList(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const nodes = useSuspenseQuery({ ...nodesQuery, ...machinePolling });
  // A member gets the directory, names only, so the share dialog and the shared-with marks can
  // name people; the owner filter is for a caller who sees other people's machines at all.
  const users = useQuery({ ...usersQuery, enabled: canListUsers(me) });
  const view = filtersFromSearch(search);
  const machines = nodes.data.nodes;
  const rows = filterNodes(machines, view);
  const deferred = useDeferredValue(view.query);
  const mayAct = can(me, "devices:core");
  const mayFilterByUser = can(me, "users:read") && can(me, "devices:core:read");

  const table = useAppTable({
    data: rows,
    columns: mayAct ? selectableColumns : columns,
    getRowId: (node) => node.id,
    state: { globalFilter: deferred },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  // What a bulk action reaches is what the operator can see: the chips and the search box both
  // narrow the list, but only the chips narrow `rows`, so the ticks and the candidates come from
  // the table's filtered model instead — every matching machine, paging aside.
  const matching = table.getFilteredRowModel().rows;
  const selection = useMachineSelection(matching.map((row) => row.id));
  const shown = table.getRowModel().rows.length;

  // Typing narrows the list as the operator types, so a keystroke replaces the URL rather than
  // filling the back button with every prefix of the search.
  function setFilters(next: MachineFilterState, typed = false): void {
    void navigate({ search: () => searchFromFilters(next), replace: typed });
  }

  function clearFilters(): void {
    void navigate({ search: {} });
  }

  // Adding a machine is a page of its own, the Mesh "Add node" wizard, not a dialog.
  function addMachine(): void {
    void navigate({ to: "/machines/add" });
  }

  return (
    <>
      <PageHeader
        title="Machines"
        docs="ref/device-management"
        description={can(me, "devices:core:read") ? machinesNote : ownMachinesNote}
        meta={summary(machines)}
      />
      <MachinesToolbar
        me={me}
        query={view.query}
        onAddMachine={addMachine}
        status={view.status}
        user={view.user}
        users={mayFilterByUser ? users.data?.users : undefined}
        tags={tagOptions(machines)}
        tag={view.tag}
        attestation={view.attestation}
        attestable={anyAttestation(machines)}
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
        onAttestationChange={(value) => {
          setFilters({ ...view, attestation: value });
        }}
      />
      <TableGroup className="@container">
        <FilterChips
          chips={machineChips(view, mayFilterByUser ? users.data?.users : undefined, setFilters)}
          onClearAll={clearFilters}
        />
        <SelectionProvider selection={selection}>
          <DataTable
            table={table}
            headerOverlay={
              <MachineBulkBar selection={selection} nodes={matching.map((row) => row.original)} />
            }
            overlayShown={selection.selected.size > 0}
            footerPlacement="inside"
            onRowClick={(nodeId) => {
              void navigate({ to: "/machines/$nodeId", params: { nodeId } });
            }}
            empty={
              <MachinesEmpty
                total={machines.length}
                status={view.status}
                narrowed={
                  view.query !== "" ||
                  view.user !== "" ||
                  view.tag !== "" ||
                  view.attestation !== defaultAttestation
                }
                ownOnly={!can(me, "devices:core:read")}
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
        </SelectionProvider>
      </TableGroup>
    </>
  );
}

/** The rail beside an empty tailnet: what people join machines for, each with its guide. */
function UseCases(): ReactElement {
  return (
    <RailSection title="Common use cases" size="lg">
      <p className="-mt-1 text-kumo-subtle">
        How a tailnet fits into the machines you already run.
      </p>
      <RailList>
        <RailRow
          title="Connect your devices"
          description="Install Tailscale on a laptop or phone and sign in to this server."
          link={{
            href: docsUrl("usage/getting-started"),
            label: "Getting started",
            external: true,
          }}
        />
        <RailRow
          title="Reach a private network"
          description="Let one machine route a subnet, so the rest reach it without a client of their own."
          link={{ href: docsUrl("ref/routes"), label: "Subnet routes", external: true }}
        />
        <RailRow
          title="Join servers without a person"
          description="Hand a server a pre-auth key and a tag, so it belongs to the tailnet rather than a user."
          link={{ href: docsUrl("ref/tags"), label: "Tags", external: true }}
        />
      </RailList>
    </RailSection>
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
