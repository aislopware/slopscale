import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { KeyIcon, PlusIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement, ReactNode } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { apiKeysQuery, groupsQuery, preAuthKeysQuery, usersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { apiKeyColumns, emptyUsers } from "~/components/keys/api-columns.tsx";
import { CreateApiKeyDialog } from "~/components/keys/api-dialogs.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { apiKeyStatus, preAuthKeyStatus } from "~/components/keys/status.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

/** The table already draws the card edge, and a row of the table is no place for a page-sized title. */
const emptyClass = "border-none bg-kumo-base [&>h2]:text-base";
const emptyIconSize = 32;

const tabs = ["preauth", "api"] as const;
type TabValue = (typeof tabs)[number];

const statuses = ["all", "active", "used", "expired"] as const;
type StatusFilter = (typeof statuses)[number];

const preAuthStatusTabs: readonly { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "used", label: "Used" },
  { value: "expired", label: "Expired" },
];

const apiStatusTabs = preAuthStatusTabs.filter((tab) => tab.value !== "used");

function toTab(value: unknown): TabValue | undefined {
  return tabs.find((known) => known === value);
}

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function toStatus(value: unknown): StatusFilter | undefined {
  return statuses.find((known) => known === value);
}

/**
 * Every parameter reads as "absent means the default", so an untouched control never reaches the
 * URL and an unknown value in a hand-written link is ignored instead of failing the route.
 */
/** One reusable `unknown()` so each entry is only a pipe over a named transform. */
const anyValue = unknown();

const optionalTab = optional(pipe(anyValue, transform(toTab)));
const optionalText = optional(pipe(anyValue, transform(toText)));
const optionalStatus = optional(pipe(anyValue, transform(toStatus)));

const searchSchema = object({ tab: optionalTab, q: optionalText, status: optionalStatus });

interface KeysSearch {
  readonly tab: TabValue | undefined;
  readonly q: string | undefined;
  readonly status: StatusFilter | undefined;
}

function searchFor(tab: TabValue, query: string, status: StatusFilter): KeysSearch {
  return {
    tab: tab === "preauth" ? undefined : tab,
    q: query === "" ? undefined : query,
    status: status === "all" ? undefined : status,
  };
}

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

function KeysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const mayReadPreAuth = can(me, "auth_keys:read");
  const tab: TabValue = mayReadPreAuth ? (search.tab ?? "preauth") : "api";

  const controls: PanelControls = {
    query: search.q ?? "",
    status: search.status ?? "all",
    handleQueryChange: (value) => {
      void navigate({ search: () => searchFor(tab, value, search.status ?? "all"), replace: true });
    },
    handleStatusChange: (value) => {
      void navigate({ search: () => searchFor(tab, search.q ?? "", toStatus(value) ?? "all") });
    },
    handleClear: () => {
      void navigate({ search: () => searchFor(tab, "", "all"), replace: true });
    },
  };

  // Filters belong to the table below, so switching tables clears them.
  const handleTabChange = (next: string): void => {
    void navigate({ search: () => searchFor(next === "api" ? "api" : "preauth", "", "all") });
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
      <div className="flex flex-col gap-4">
        <Tabs variant="underline" tabs={tabItems} value={tab} onValueChange={handleTabChange} />
        {tab === "preauth" && mayReadPreAuth ? (
          <PreAuthPanel me={me} controls={controls} />
        ) : (
          <ApiPanel me={me} controls={controls} />
        )}
      </div>
    </>
  );
}

interface PanelControls {
  readonly query: string;
  readonly status: StatusFilter;
  readonly handleQueryChange: (value: string) => void;
  readonly handleStatusChange: (value: string) => void;
  /** Clears search and status, keeping the open tab. */
  readonly handleClear: () => void;
}

function PreAuthPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const keys = useSuspenseQuery(preAuthKeysQuery);
  // Group names for the type cell; without the scope the cell shows the ids.
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const [creating, setCreating] = useState(false);
  const filter = useDeferredValue(controls.query);
  const rows = keys.data.preAuthKeys.filter(
    (authKey) => controls.status === "all" || preAuthKeyStatus(authKey) === controls.status,
  );
  const table = useAppTable({
    data: rows,
    columns: preAuthKeyColumns,
    getRowId: (authKey) => authKey.id,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, ...(groups.data === undefined ? {} : { groups: groups.data.groups }) },
  });
  const create = (): void => {
    setCreating(true);
  };

  return (
    <KeyPanel
      controls={controls}
      statusTabs={preAuthStatusTabs}
      placeholder="Search by key, user or tag"
      action={
        <Button variant="primary" icon={PlusIcon} disabled={!can(me, "auth_keys")} onClick={create}>
          Create key
        </Button>
      }
    >
      <table.AppTable>
        <PanelTable
          controls={controls}
          total={keys.data.preAuthKeys.length}
          shown={table.getRowModel().rows.length}
          noun="pre-auth key"
          firstEmpty={
            <Empty
              className={emptyClass}
              size="sm"
              icon={<KeyIcon size={emptyIconSize} />}
              title="No pre-auth keys"
              description="A pre-auth key lets a machine register without anyone signing in on it."
              contents={
                <Button variant="primary" disabled={!can(me, "auth_keys")} onClick={create}>
                  Create key
                </Button>
              }
            />
          }
        />
      </table.AppTable>
      <CreatePreAuthKeyDialog me={me} open={creating} onOpenChange={setCreating} />
    </KeyPanel>
  );
}

function ApiPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const keys = useSuspenseQuery(apiKeysQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [creating, setCreating] = useState(false);
  const filter = useDeferredValue(controls.query);
  const rows = keys.data.apiKeys.filter(
    (apiKey) => controls.status === "all" || apiKeyStatus(apiKey) === controls.status,
  );
  const table = useAppTable({
    data: rows,
    columns: apiKeyColumns,
    getRowId: (apiKey) => apiKey.id,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });
  const create = (): void => {
    setCreating(true);
  };

  return (
    <KeyPanel
      controls={controls}
      statusTabs={apiStatusTabs}
      placeholder="Search by prefix"
      action={
        <Button variant="primary" icon={PlusIcon} onClick={create}>
          Create API key
        </Button>
      }
    >
      <table.AppTable>
        <PanelTable
          controls={controls}
          total={keys.data.apiKeys.length}
          shown={table.getRowModel().rows.length}
          noun="API key"
          firstEmpty={
            <Empty
              className={emptyClass}
              size="sm"
              icon={<KeyIcon size={emptyIconSize} />}
              title="No API keys"
              description="An API key authenticates scripts and other tools against the headscale API."
              contents={
                <Button variant="primary" onClick={create}>
                  Create API key
                </Button>
              }
            />
          }
        />
      </table.AppTable>
      <CreateApiKeyDialog me={me} open={creating} onOpenChange={setCreating} />
    </KeyPanel>
  );
}

/** Card, toolbar and the panel's own table: the shape both key tables share. */
function KeyPanel({
  controls,
  statusTabs,
  placeholder,
  action,
  children,
}: {
  readonly controls: PanelControls;
  readonly statusTabs: readonly { value: StatusFilter; label: string }[];
  readonly placeholder: string;
  readonly action: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <LayerCard className="overflow-hidden">
      <TableToolbar actions={action}>
        <SearchInput
          value={controls.query}
          placeholder={placeholder}
          onValueChange={controls.handleQueryChange}
        />
        <Tabs
          variant="segmented"
          size="sm"
          tabs={[...statusTabs]}
          value={controls.status}
          onValueChange={controls.handleStatusChange}
        />
      </TableToolbar>
      {children}
    </LayerCard>
  );
}

/** "1 API key" / "3 API keys": the footer counts the rows an operator can see. */
function count(total: number, noun: string): string {
  return total === 1 ? `1 ${noun}` : `${total} ${noun}s`;
}

function PanelTable({
  controls,
  total,
  shown,
  noun,
  firstEmpty,
}: {
  readonly controls: PanelControls;
  readonly total: number;
  readonly shown: number;
  readonly noun: string;
  readonly firstEmpty: ReactNode;
}): ReactElement {
  return (
    <DataTable
      rowClassName="group/row"
      empty={
        total === 0 ? (
          firstEmpty
        ) : (
          <Empty
            className={emptyClass}
            size="sm"
            title="No keys match"
            description="No key matches this search and filter."
            contents={
              <Button variant="secondary" onClick={controls.handleClear}>
                Clear filters
              </Button>
            }
          />
        )
      }
      footer={
        total === 0 ? undefined : (
          <TableFooter>{`Showing ${shown} of ${count(total, noun)}`}</TableFooter>
        )
      }
    />
  );
}
