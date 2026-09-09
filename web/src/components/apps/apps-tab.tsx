import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { AppWindowIcon, PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { App } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { appColumns } from "~/components/apps/app-columns.tsx";
import { AppDialog } from "~/components/apps/app-dialogs.tsx";
import { countApps } from "~/components/apps/model.ts";
import { useAppMutations } from "~/components/apps/mutations.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface AppsTabProps {
  readonly me: Me;
  readonly apps: readonly App[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** The tailnet's apps: which domains each one covers and how its connectors are doing. */
export function AppsTab({ me, apps, search, onSearchChange }: AppsTabProps): ReactElement {
  const canEdit = can(me, "policy_file");
  const query = useDeferredValue(search);
  const [creating, setCreating] = useState(false);
  const mutations = useAppMutations();

  const table = useAppTable({
    data: apps,
    columns: appColumns,
    getRowId: (app) => app.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const total = apps.length;
  const shown = table.getRowModel().rows.length;

  return (
    <>
      <TableToolbar
        actions={
          <Button
            variant="primary"
            icon={PlusIcon}
            disabled={!canEdit}
            onClick={() => {
              setCreating(true);
            }}
          >
            New app
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, domain, tag or machine"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <EmptyApps
                  canEdit={canEdit}
                  onCreate={() => {
                    setCreating(true);
                  }}
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No apps match"
                  contents={
                    <Button
                      variant="secondary"
                      onClick={() => {
                        onSearchChange("");
                      }}
                    >
                      Clear search
                    </Button>
                  }
                />
              )
            }
            footer={
              total === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${countApps(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <AppDialog open={creating} onOpenChange={setCreating} mutations={mutations} />
    </>
  );
}

/**
 * Nothing to list yet, so the panel explains the two halves an app needs: the definition the
 * operator writes here, and the machine that carries the tag and runs the connector.
 */
function EmptyApps({
  canEdit,
  onCreate,
}: {
  readonly canEdit: boolean;
  readonly onCreate: () => void;
}): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      icon={<AppWindowIcon size={emptyIconSize} />}
      title="No apps yet"
      description="An app names the domains it covers and the tags of the machines that reach them. Two things make one work:"
      contents={
        <div className="flex flex-col items-center gap-4">
          <ol className="flex max-w-prose list-decimal flex-col gap-1 pl-5 text-left text-kumo-subtle">
            <li>
              Tag a machine with one of the tags the app names, such as <Code>tag:connector</Code>.
            </li>
            <li>
              Run <Code>tailscale set --advertise-connector</Code> on it.
            </li>
          </ol>
          <Button variant="primary" disabled={!canEdit} onClick={onCreate}>
            New app
          </Button>
        </div>
      }
    />
  );
}
