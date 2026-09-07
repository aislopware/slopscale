import { Empty } from "@cloudflare/kumo/components/empty";
import { UsersIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, string } from "valibot";

import { usersQuery } from "~/api/queries.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { Card, CardHeader } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { AddUserButton } from "~/components/users/add-user.tsx";
import { columns } from "~/components/users/columns.tsx";

const emptyIconSize = 40;
/** The table already draws the card edge, so the empty panel drops its own. */
const emptyClass = "border-none bg-kumo-base";

const optionalText = optional(string(), "");

const searchSchema = object({
  q: fallback(optionalText, ""),
});

export const Route = createFileRoute("/_app/users")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await context.queryClient.query(usersQuery);
  },
  component: UsersPage,
});

function UsersPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const users = useSuspenseQuery(usersQuery);
  const query = useDeferredValue(search.q);

  const table = useAppTable({
    data: users.data.users,
    columns,
    getRowId: (user) => user.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const pending = users.data.users.filter((user) => !user.approved).length;

  return (
    <>
      <PageHeader
        title="Users"
        description={describe(users.data.users.length, pending)}
        actions={<AddUserButton me={me} />}
      />
      <Card>
        <CardHeader className="items-center">
          <SearchInput
            value={search.q}
            placeholder="Search by name, email or role"
            onValueChange={(value) => {
              void navigate({ search: () => ({ q: value }), replace: true });
            }}
          />
        </CardHeader>
        <table.AppTable>
          <DataTable
            empty={
              users.data.users.length === 0 ? (
                <Empty
                  className={emptyClass}
                  icon={<UsersIcon size={emptyIconSize} />}
                  title="No users yet"
                  description="A user appears here after their first sign-in; you can also add one now and hand out a pre-auth key."
                />
              ) : (
                <Empty
                  className={emptyClass}
                  title="No users match"
                  description="Try a different search."
                  size="sm"
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
  const users = total === 1 ? "1 user" : `${total} users`;

  return pending === 0 ? users : `${users}, ${pending} waiting for approval`;
}
