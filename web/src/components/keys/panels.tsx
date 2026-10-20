import { Button } from "@cloudflare/kumo/components/button";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { TabsItem } from "@cloudflare/kumo/components/tabs";
import { PlusIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useDeferredValue, useState } from "react";
import type { ReactElement, ReactNode } from "react";

import {
  apiKeysQuery,
  groupsQuery,
  oauthClientsQuery,
  preAuthKeysQuery,
  usersQuery,
} from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { apiKeyColumns, emptyUsers } from "~/components/keys/api-columns.tsx";
import { matchesKind } from "~/components/keys/federated.ts";
import { oauthClientColumns } from "~/components/keys/oauth-columns.tsx";
import { CreateFederatedIdentityDialog } from "~/components/keys/oauth-dialogs.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import type { KindFilter } from "~/components/keys/search.ts";
import { apiKeyStatus, preAuthKeyStatus } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import type { AppTableInstance, RowData } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { countedTabs } from "~/components/table/tab-count.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

const preAuthStatusTabs: readonly { value: StatusFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "used", label: "Used" },
  { value: "expired", label: "Expired" },
];

const apiStatusTabs = preAuthStatusTabs.filter((tab) => tab.value !== "used");

export interface PanelControls {
  readonly query: string;
  readonly status: StatusFilter;
  readonly kind: KindFilter;
  readonly handleQueryChange: (value: string) => void;
  readonly handleStatusChange: (value: string) => void;
  readonly handleKindChange: (value: string) => void;
  /** Clears search and status. */
  readonly handleClear: () => void;
}

/**
 * The pre-auth keys list. Creating one is a page of its own, linked from the page header, as the
 * dashboard's "Create Token" is.
 */
export function PreAuthPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const keys = useSuspenseQuery(preAuthKeysQuery);
  // Group names for the type cell; without the scope the cell shows the ids.
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const filter = useDeferredValue(controls.query);
  const statusOf = (status: StatusFilter): number =>
    keys.data.preAuthKeys.filter(
      (authKey) => status === "all" || preAuthKeyStatus(authKey) === status,
    ).length;
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

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(preAuthStatusTabs, statusOf)}
      placeholder="Search by key, user or tag"
    >
      <PanelTable
        table={table}
        controls={controls}
        total={keys.data.preAuthKeys.length}
        firstEmpty={
          <SectionEmpty
            title="No pre-auth keys"
            description="A pre-auth key lets a machine register without anyone signing in on it."
          />
        }
      />
    </KeyPanel>
  );
}

/** The API keys list; creating one is a page of its own, linked from the page header. */
export function ApiPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const keys = useSuspenseQuery(apiKeysQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const filter = useDeferredValue(controls.query);
  const statusOf = (status: StatusFilter): number =>
    keys.data.apiKeys.filter((apiKey) => status === "all" || apiKeyStatus(apiKey) === status)
      .length;
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

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(apiStatusTabs, statusOf)}
      placeholder="Search by prefix"
    >
      <PanelTable
        table={table}
        controls={controls}
        total={keys.data.apiKeys.length}
        firstEmpty={
          <SectionEmpty
            title="No API keys"
            description="An API key authenticates scripts and other tools against the v1 API."
          />
        }
      />
    </KeyPanel>
  );
}

const oauthKindTabs: readonly { value: KindFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "client", label: "Clients" },
  { value: "federated", label: "Federated" },
];

/**
 * The OAuth clients and federated identities list. A client is created on a page of its own; a
 * federated identity still in its dialog, opened from the page header's secondary action.
 */
export function OAuthPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const clients = useSuspenseQuery(oauthClientsQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const filter = useDeferredValue(controls.query);

  const kindOf = (kind: KindFilter): number =>
    clients.data.oauthClients.filter((client) => matchesKind(client, kind)).length;

  const rows = clients.data.oauthClients.filter((client) => matchesKind(client, controls.kind));

  const table = useAppTable({
    data: rows,
    columns: oauthClientColumns,
    getRowId: (client) => client.clientId,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(oauthKindTabs, kindOf)}
      tabValue={controls.kind}
      onTabChange={controls.handleKindChange}
      placeholder="Search by client, subject or issuer"
    >
      <PanelTable
        table={table}
        controls={controls}
        total={clients.data.oauthClients.length}
        firstEmpty={
          <SectionEmpty
            title="No OAuth clients"
            description="An OAuth client or federated identity lets automation get short-lived v2 API tokens."
          />
        }
      />
    </KeyPanel>
  );
}

/** The OAuth page header's secondary action: the federated identity dialog and its trigger. */
export function NewFederatedIdentityButton({ me }: { readonly me: Me }): ReactElement {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button
        variant="secondary"
        icon={PlusIcon}
        disabled={!can(me, "oauth_keys")}
        onClick={() => {
          setOpen(true);
        }}
      >
        New federated identity
      </Button>
      <CreateFederatedIdentityDialog me={me} open={open} onOpenChange={setOpen} />
    </>
  );
}

/** Toolbar and the panel's own table: the shape the key tables share. */
function KeyPanel({
  controls,
  tabs,
  tabValue,
  onTabChange,
  placeholder,
  children,
}: {
  readonly controls: PanelControls;
  /** Absent for a table whose rows have no category filters. */
  readonly tabs?: readonly TabsItem[] | undefined;
  readonly tabValue?: string | undefined;
  readonly onTabChange?: ((value: string) => void) | undefined;
  readonly placeholder: string;
  readonly children: ReactNode;
}): ReactElement {
  const currentTab = tabValue ?? controls.status;
  const handleTab = onTabChange ?? controls.handleStatusChange;

  return (
    <>
      <TableToolbar>
        <SearchInput
          value={controls.query}
          placeholder={placeholder}
          onValueChange={controls.handleQueryChange}
        />
        {tabs === undefined ? null : (
          <Tabs variant="segmented" tabs={[...tabs]} value={currentTab} onValueChange={handleTab} />
        )}
      </TableToolbar>
      <TableGroup>{children}</TableGroup>
    </>
  );
}

function PanelTable<TData extends RowData>({
  table,
  controls,
  total,
  firstEmpty,
}: {
  readonly table: AppTableInstance<TData>;
  readonly controls: PanelControls;
  readonly total: number;
  readonly firstEmpty: ReactNode;
}): ReactElement {
  return (
    <DataTable
      table={table}
      empty={
        total === 0 ? (
          firstEmpty
        ) : (
          <SectionEmpty
            title="No keys match"
            contents={
              <Button variant="secondary" onClick={controls.handleClear}>
                Clear filters
              </Button>
            }
          />
        )
      }
    />
  );
}
