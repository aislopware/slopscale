import { LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Select } from "@cloudflare/kumo/components/select";
import { DevicesIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, string } from "valibot";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { columns, emptyUsers } from "~/components/machines/columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { Card } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { nodeStatus, userLabel } from "~/lib/node.ts";

const statuses = ["all", "online", "offline", "pending", "expired"] as const;
type StatusFilter = (typeof statuses)[number];

const optionalText = optional(string(), "");
const optionalStatus = optional(picklist(statuses), "all");

const searchSchema = object({
  q: fallback(optionalText, ""),
  status: fallback(optionalStatus, "all"),
  user: fallback(optionalText, ""),
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

const statusOptions: readonly { value: StatusFilter; label: string }[] = [
  { value: "all", label: "Any status" },
  { value: "online", label: "Connected" },
  { value: "offline", label: "Disconnected" },
  { value: "pending", label: "Needs approval" },
  { value: "expired", label: "Expired" },
];

const emptyIconSize = 48;

function MachinesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const nodes = useSuspenseQuery(nodesQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const rows = filterNodes(nodes.data.nodes, search.status, search.user);
  const query = useDeferredValue(search.q);

  const table = useAppTable({
    data: rows,
    columns,
    getRowId: (node) => node.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  const pending = nodes.data.nodes.filter((node) => !node.approved).length;

  return (
    <>
      <PageHeader title="Machines" description={describe(nodes.data.nodes.length, pending)} />
      <Card>
        <div className="flex flex-wrap items-center gap-2 border-b border-kumo-line px-5 py-3">
          <SearchInput
            value={search.q}
            placeholder="Search by name, address, user or tag"
            onValueChange={(value) => {
              void navigate({ search: (previous) => ({ ...previous, q: value }), replace: true });
            }}
          />
          <Select
            aria-label="Filter by status"
            className="w-40"
            value={search.status}
            items={statusOptions}
            onValueChange={(value) => {
              void navigate({ search: (previous) => ({ ...previous, status: value ?? "all" }) });
            }}
          />
          {users.data === undefined ? null : (
            <Select
              aria-label="Filter by user"
              className="w-48"
              value={search.user}
              items={userOptions(users.data.users)}
              onValueChange={(value) => {
                void navigate({ search: (previous) => ({ ...previous, user: value ?? "" }) });
              }}
            />
          )}
        </div>
        <table.AppTable>
          <DataTable
            onRowClick={(nodeId) => {
              void navigate({ to: "/machines/$nodeId", params: { nodeId } });
            }}
            empty={
              nodes.data.nodes.length === 0 ? (
                <Empty
                  size="sm"
                  icon={<DevicesIcon size={emptyIconSize} />}
                  title="No machines yet"
                  description="Register a device with a pre-auth key or by signing in; it appears here immediately."
                  contents={
                    can(me, "auth_keys") ? (
                      <LinkButton href="/keys" variant="secondary">
                        Create a pre-auth key
                      </LinkButton>
                    ) : null
                  }
                />
              ) : (
                <Empty
                  size="sm"
                  title="No machines match"
                  description="Try a different search or filter."
                />
              )
            }
          />
        </table.AppTable>
      </Card>
    </>
  );
}

function describe(total: number, pending: number): string {
  const machines = total === 1 ? "1 machine" : `${total} machines`;

  return pending === 0 ? machines : `${machines}, ${pending} waiting for approval`;
}

function filterNodes(nodes: readonly Node[], status: StatusFilter, user: string): Node[] {
  const now = new Date();

  return nodes.filter((node) => {
    if (user !== "" && node.user.id !== user && !node.sharedWith.includes(user)) {
      return false;
    }

    return status === "all" || nodeStatus(node, now) === status;
  });
}

function userOptions(users: readonly User[]): { value: string; label: string }[] {
  return [
    { value: "", label: "Any user" },
    ...users.map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}
