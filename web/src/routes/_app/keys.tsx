import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { KeyIcon, PlusIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";
import { fallback, object, optional, picklist, string } from "valibot";

import { apiKeysQuery, preAuthKeysQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { apiKeyColumns, emptyUsers } from "~/components/keys/api-columns.tsx";
import { CreateApiKeyDialog } from "~/components/keys/api-dialogs.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { Card, CardHeader } from "~/components/ui/card.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const emptyIconSize = 40;
/** The table already draws the card edge, so the empty panel drops its own. */
const emptyClass = "border-none bg-kumo-base";

const tabs = ["preauth", "api"] as const;
type TabValue = (typeof tabs)[number];

const optionalTab = optional(picklist(tabs), "preauth");
const optionalText = optional(string(), "");

const searchSchema = object({
  tab: fallback(optionalTab, "preauth"),
  q: fallback(optionalText, ""),
});

export const Route = createFileRoute("/_app/keys")({
  validateSearch: searchSchema,
  loaderDeps: () => ({}),
  loader: async ({ context }) => {
    await Promise.all([
      can(context.me, "auth_keys:read")
        ? context.queryClient.query(preAuthKeysQuery)
        : Promise.resolve(),
      context.queryClient.query(apiKeysQuery),
    ]);
  },
  component: KeysPage,
});

function asTab(value: string): TabValue {
  return value === "api" ? "api" : "preauth";
}

function KeysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const mayReadPreAuth = can(me, "auth_keys:read");
  const tab: TabValue = mayReadPreAuth ? search.tab : "api";

  const setQuery = (value: string): void => {
    void navigate({ search: (previous) => ({ ...previous, q: value }), replace: true });
  };

  const tabItems = [
    ...(mayReadPreAuth ? [{ value: "preauth", label: "Pre-auth keys" }] : []),
    { value: "api", label: "API keys" },
  ];

  return (
    <>
      <PageHeader
        title="Keys"
        description="Pre-auth keys register machines without a login; API keys authenticate this console and automation."
      />
      <div className="flex flex-col gap-5">
        <Tabs
          variant="underline"
          tabs={tabItems}
          value={tab}
          onValueChange={(next) => {
            void navigate({ search: (previous) => ({ ...previous, tab: asTab(next) }) });
          }}
        />
        {tab === "preauth" && mayReadPreAuth ? (
          <PreAuthPanel me={me} query={search.q} onQueryChange={setQuery} />
        ) : null}
        {tab === "api" ? <ApiPanel me={me} query={search.q} onQueryChange={setQuery} /> : null}
      </div>
    </>
  );
}

interface PanelProps {
  readonly me: Me;
  readonly query: string;
  readonly onQueryChange: (value: string) => void;
}

function PreAuthPanel({ me, query, onQueryChange }: PanelProps): ReactElement {
  const keys = useSuspenseQuery(preAuthKeysQuery);
  const [creating, setCreating] = useState(false);
  const filter = useDeferredValue(query);
  const table = useAppTable({
    data: keys.data.preAuthKeys,
    columns: preAuthKeyColumns,
    getRowId: (authKey) => authKey.id,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me },
  });

  return (
    <Card>
      <CardHeader className="items-center">
        <SearchInput
          value={query}
          placeholder="Search by key, user or tag"
          onValueChange={onQueryChange}
        />
        <Button
          variant="primary"
          icon={PlusIcon}
          disabled={!can(me, "auth_keys")}
          onClick={() => {
            setCreating(true);
          }}
        >
          Create key
        </Button>
      </CardHeader>
      <table.AppTable>
        <DataTable
          empty={
            keys.data.preAuthKeys.length === 0 ? (
              <Empty
                className={emptyClass}
                icon={<KeyIcon size={emptyIconSize} />}
                title="No pre-auth keys"
                description="A pre-auth key lets a machine register without anyone signing in on it."
              />
            ) : (
              <Empty
                className={emptyClass}
                title="No keys match"
                description="Try a different search."
                size="sm"
              />
            )
          }
        />
      </table.AppTable>
      <CreatePreAuthKeyDialog me={me} open={creating} onOpenChange={setCreating} />
    </Card>
  );
}

function ApiPanel({ me, query, onQueryChange }: PanelProps): ReactElement {
  const keys = useSuspenseQuery(apiKeysQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [creating, setCreating] = useState(false);
  const filter = useDeferredValue(query);
  const table = useAppTable({
    data: keys.data.apiKeys,
    columns: apiKeyColumns,
    getRowId: (apiKey) => apiKey.id,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  return (
    <Card>
      <CardHeader className="items-center">
        <SearchInput value={query} placeholder="Search by prefix" onValueChange={onQueryChange} />
        <Button
          variant="primary"
          icon={PlusIcon}
          onClick={() => {
            setCreating(true);
          }}
        >
          Create API key
        </Button>
      </CardHeader>
      <table.AppTable>
        <DataTable
          empty={
            keys.data.apiKeys.length === 0 ? (
              <Empty
                className={emptyClass}
                icon={<KeyIcon size={emptyIconSize} />}
                title="No API keys"
                description="An API key authenticates scripts and other tools against the headscale API."
              />
            ) : (
              <Empty
                className={emptyClass}
                title="No keys match"
                description="Try a different search."
                size="sm"
              />
            )
          }
        />
      </table.AppTable>
      <CreateApiKeyDialog me={me} open={creating} onOpenChange={setCreating} />
    </Card>
  );
}
