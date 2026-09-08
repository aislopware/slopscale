import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { UsersIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { groupsQuery, usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { countedTabs } from "~/components/table/tab-count.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { AddUserButton } from "~/components/users/add-user.tsx";
import { columns } from "~/components/users/columns.tsx";
import { CreateUserDialog } from "~/components/users/dialogs.tsx";
import { useUserMutations } from "~/components/users/mutations.ts";
import { isAdminRole } from "~/components/users/roles.ts";

const filters = ["all", "admins", "pending"] as const;
type UserFilter = (typeof filters)[number];

const filterTabs: readonly { value: UserFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "admins", label: "Admins" },
  { value: "pending", label: "Pending" },
];

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function toFilter(value: unknown): UserFilter | undefined {
  return filters.find((known) => known === value);
}

/**
 * Both parameters read as "absent means the default", so a filter left alone never reaches the URL
 * and an unknown value in a hand-written link is ignored instead of failing the route.
 */
/** One reusable `unknown()` so each entry is only a pipe over a named transform. */
const anyValue = unknown();

const optionalText = optional(pipe(anyValue, transform(toText)));
const optionalFilter = optional(pipe(anyValue, transform(toFilter)));

const searchSchema = object({ q: optionalText, filter: optionalFilter });

interface UsersSearch {
  readonly q: string | undefined;
  readonly filter: UserFilter | undefined;
}

function searchFor(query: string, filter: UserFilter): UsersSearch {
  return {
    q: query === "" ? undefined : query,
    filter: filter === "all" ? undefined : filter,
  };
}

export const Route = createFileRoute("/_app/users")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(usersQuery),
      can(context.me, "policy_file:read")
        ? context.queryClient.query(groupsQuery)
        : Promise.resolve(),
    ]);
  },
  component: UsersPage,
});

function matchesFilter(user: User, filter: UserFilter): boolean {
  if (filter === "admins") {
    return isAdminRole(user.role);
  }

  return filter === "pending" ? !user.approved : true;
}

function UsersPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const users = useSuspenseQuery(usersQuery);
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const text = search.q ?? "";
  const filter = search.filter ?? "all";
  const query = useDeferredValue(text);
  const rows = users.data.users.filter((user) => matchesFilter(user, filter));

  const table = useAppTable({
    data: rows,
    columns,
    getRowId: (user) => user.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, ...(groups.data === undefined ? {} : { groups: groups.data.groups }) },
  });

  const total = users.data.users.length;
  const shown = table.getRowModel().rows.length;
  const pending = users.data.users.filter((user) => !user.approved).length;
  const handleSearchChange = (value: string): void => {
    void navigate({ search: () => searchFor(value, filter), replace: true });
  };
  const handleFilterChange = (value: string): void => {
    void navigate({ search: () => searchFor(text, toFilter(value) ?? "all") });
  };
  const handleClear = (): void => {
    void navigate({ search: () => searchFor("", "all"), replace: true });
  };

  return (
    <>
      <PageHeader title="Users" meta={describe(total, pending)} />
      <Frame>
        <TableToolbar actions={<AddUserButton me={me} />}>
          <SearchInput
            value={text}
            placeholder="Search by name, email or role"
            onValueChange={handleSearchChange}
          />
          <Tabs
            variant="segmented"
            tabs={countedTabs(
              filterTabs,
              (value) => users.data.users.filter((user) => matchesFilter(user, value)).length,
            )}
            value={filter}
            onValueChange={handleFilterChange}
          />
        </TableToolbar>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <FirstUserEmpty me={me} />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No users match"
                  description="No user matches this search and filter."
                  contents={
                    <Button variant="secondary" onClick={handleClear}>
                      Clear filters
                    </Button>
                  }
                />
              )
            }
            footer={
              total === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${countUsers(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
    </>
  );
}

/** The only empty state that can offer something: there is not a single user yet. */
function FirstUserEmpty({ me }: { readonly me: Me }): ReactElement {
  const [open, setOpen] = useState(false);
  const mutations = useUserMutations();

  return (
    <>
      <Empty
        className={tableEmptyClass}
        size="sm"
        icon={<UsersIcon size={emptyIconSize} />}
        title="No users yet"
        description="A user appears here after their first sign-in, or you can add one now and hand out a pre-auth key."
        contents={
          <Button
            variant="primary"
            disabled={!can(me, "users")}
            onClick={() => {
              setOpen(true);
            }}
          >
            Add user
          </Button>
        }
      />
      <CreateUserDialog open={open} onOpenChange={setOpen} mutations={mutations} />
    </>
  );
}

function countUsers(total: number): string {
  return total === 1 ? "1 user" : `${total} users`;
}

function describe(total: number, pending: number): string {
  const users = countUsers(total);

  return pending === 0 ? users : `${users} · ${pending} waiting for approval`;
}
