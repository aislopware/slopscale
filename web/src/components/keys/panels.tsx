import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { TabsItem } from "@cloudflare/kumo/components/tabs";
import { KeyIcon, PlugsConnectedIcon, PlusIcon } from "@phosphor-icons/react";
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
import { oauthClientColumns } from "~/components/keys/oauth-columns.tsx";
import { CreateOAuthClientDialog } from "~/components/keys/oauth-dialogs.tsx";
import { preAuthKeyColumns } from "~/components/keys/preauth-columns.tsx";
import { CreatePreAuthKeyDialog } from "~/components/keys/preauth-dialogs.tsx";
import { apiKeyStatus, preAuthKeyStatus } from "~/components/keys/status.ts";
import type { StatusFilter } from "~/components/keys/status.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
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
  readonly handleQueryChange: (value: string) => void;
  readonly handleStatusChange: (value: string) => void;
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
      statusTabs={countedTabs(preAuthStatusTabs, statusOf)}
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
              icon={<KeyIcon size={emptyIconSize} />}
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
      statusTabs={countedTabs(apiStatusTabs, statusOf)}
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
              icon={<KeyIcon size={emptyIconSize} />}
              title="No API keys"
              description="An API key authenticates scripts and other tools against the headscale API."
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

export function OAuthPanel({
  me,
  controls,
}: {
  readonly me: Me;
  readonly controls: PanelControls;
}): ReactElement {
  const clients = useSuspenseQuery(oauthClientsQuery);
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const [creating, setCreating] = useState(false);
  const filter = useDeferredValue(controls.query);
  const table = useAppTable({
    data: clients.data.oauthClients,
    columns: oauthClientColumns,
    getRowId: (client) => client.clientId,
    state: { globalFilter: filter },
    initialState: { sorting: [{ id: "created", desc: true }] },
    meta: { me, users: users.data?.users ?? emptyUsers },
  });
  const canCreate = can(me, "oauth_keys");
  const create = (): void => {
    setCreating(true);
  };

  return (
    <KeyPanel
      controls={controls}
      placeholder="Search by client id or description"
      action={
        <Button variant="primary" icon={PlusIcon} disabled={!canCreate} onClick={create}>
          Create client
        </Button>
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
              icon={<PlugsConnectedIcon size={emptyIconSize} />}
              title="No OAuth clients"
              description="An OAuth client lets automation mint short-lived tokens for the v2 API with a secret instead of an API key."
              contents={
                <Button variant="secondary" disabled={!canCreate} onClick={create}>
                  Create client
                </Button>
              }
            />
          }
        />
      </table.AppTable>
      <CreateOAuthClientDialog me={me} open={creating} onOpenChange={setCreating} />
    </KeyPanel>
  );
}

/** Card, toolbar and the panel's own table: the shape the key tables share. */
function KeyPanel({
  controls,
  statusTabs,
  placeholder,
  action,
  children,
}: {
  readonly controls: PanelControls;
  /** Absent for a table whose rows have no status, such as OAuth clients. */
  readonly statusTabs?: readonly TabsItem[];
  readonly placeholder: string;
  readonly action: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <>
      <TableToolbar actions={action}>
        <SearchInput
          value={controls.query}
          placeholder={placeholder}
          onValueChange={controls.handleQueryChange}
        />
        {statusTabs === undefined ? null : (
          <Tabs
            variant="segmented"
            tabs={[...statusTabs]}
            value={controls.status}
            onValueChange={controls.handleStatusChange}
          />
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
