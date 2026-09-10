import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
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
import { CreateApiKeyDialog } from "~/components/keys/api-dialogs.tsx";
import { matchesKind } from "~/components/keys/federated.ts";
import { oauthClientColumns } from "~/components/keys/oauth-columns.tsx";
import {
  CreateFederatedIdentityDialog,
  CreateOAuthClientDialog,
} from "~/components/keys/oauth-dialogs.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import type { KindFilter } from "~/components/keys/search.ts";
import { apiKeyStatus, preAuthKeyStatus } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { countedTabs } from "~/components/table/tab-count.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

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
  const [creating, setCreating] = useState(false);
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
  const create = (): void => {
    setCreating(true);
  };

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(preAuthStatusTabs, statusOf)}
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
              className={tableEmptyClass}
              size="sm"
              title="No pre-auth keys"
              description="A pre-auth key lets a machine register without anyone signing in on it."
              contents={
                <Button variant="secondary" disabled={!can(me, "auth_keys")} onClick={create}>
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

export function ApiPanel({
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
  const create = (): void => {
    setCreating(true);
  };

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(apiStatusTabs, statusOf)}
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
              className={tableEmptyClass}
              size="sm"
              title="No API keys"
              description="An API key authenticates scripts and other tools against the v1 API."
              contents={
                <Button variant="secondary" onClick={create}>
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

const oauthKindTabs: readonly { value: KindFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "client", label: "Clients" },
  { value: "federated", label: "Federated" },
];

export function OAuthPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const clients = useSuspenseQuery(oauthClientsQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [creatingClient, setCreatingClient] = useState(false);
  const [creatingFederated, setCreatingFederated] = useState(false);
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
  const canCreate = can(me, "oauth_keys");

  return (
    <KeyPanel
      controls={controls}
      tabs={countedTabs(oauthKindTabs, kindOf)}
      tabValue={controls.kind}
      onTabChange={controls.handleKindChange}
      placeholder="Search by client, subject or issuer"
      action={
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            icon={PlusIcon}
            disabled={!canCreate}
            onClick={() => {
              setCreatingFederated(true);
            }}
          >
            New federated identity
          </Button>
          <Button
            variant="primary"
            icon={PlusIcon}
            disabled={!canCreate}
            onClick={() => {
              setCreatingClient(true);
            }}
          >
            New OAuth client
          </Button>
        </div>
      }
    >
      <table.AppTable>
        <PanelTable
          controls={controls}
          total={clients.data.oauthClients.length}
          shown={table.getRowModel().rows.length}
          noun="OAuth client"
          firstEmpty={
            <Empty
              className={tableEmptyClass}
              size="sm"
              title="No OAuth clients"
              description="An OAuth client or federated identity lets automation get short-lived v2 API tokens."
              contents={
                <div className="flex flex-wrap items-center justify-center gap-2">
                  <Button
                    variant="secondary"
                    disabled={!canCreate}
                    onClick={() => {
                      setCreatingFederated(true);
                    }}
                  >
                    New federated identity
                  </Button>
                  <Button
                    variant="primary"
                    disabled={!canCreate}
                    onClick={() => {
                      setCreatingClient(true);
                    }}
                  >
                    New OAuth client
                  </Button>
                </div>
              }
            />
          }
        />
      </table.AppTable>
      <CreateOAuthClientDialog me={me} open={creatingClient} onOpenChange={setCreatingClient} />
      <CreateFederatedIdentityDialog
        me={me}
        open={creatingFederated}
        onOpenChange={setCreatingFederated}
      />
    </KeyPanel>
  );
}

/** Card, toolbar and the panel's own table: the shape the key tables share. */
function KeyPanel({
  controls,
  tabs,
  tabValue,
  onTabChange,
  placeholder,
  action,
  children,
}: {
  readonly controls: PanelControls;
  /** Absent for a table whose rows have no category filters. */
  readonly tabs?: readonly TabsItem[] | undefined;
  readonly tabValue?: string | undefined;
  readonly onTabChange?: ((value: string) => void) | undefined;
  readonly placeholder: string;
  readonly action: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  const currentTab = tabValue ?? controls.status;
  const handleTab = onTabChange ?? controls.handleStatusChange;

  return (
    <>
      <TableToolbar actions={action}>
        <SearchInput
          value={controls.query}
          placeholder={placeholder}
          onValueChange={controls.handleQueryChange}
        />
        {tabs === undefined ? null : (
          <Tabs variant="segmented" tabs={[...tabs]} value={currentTab} onValueChange={handleTab} />
        )}
      </TableToolbar>
      <Frame>{children}</Frame>
    </>
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
      empty={
        total === 0 ? (
          firstEmpty
        ) : (
          <Empty
            className={tableEmptyClass}
            size="sm"
            title="No keys match"
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
