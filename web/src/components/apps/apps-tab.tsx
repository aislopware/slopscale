import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { App } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { appColumns } from "~/components/apps/app-columns.tsx";
import type { AppRow } from "~/components/apps/app-columns.tsx";
import { AppDialog } from "~/components/apps/app-dialogs.tsx";
import { useLearnedCounts } from "~/components/apps/learned.ts";
import { connectorMachineCounts, totalPendingRoutes } from "~/components/apps/model.ts";
import { useAppMutations } from "~/components/apps/mutations.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { Code } from "~/components/ui/code.tsx";
import { FacetChips } from "~/components/ui/facet-chips.tsx";
import type { Facet } from "~/components/ui/facet-chips.tsx";

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
  const learned = useLearnedCounts(apps, me);
  const [facet, setFacet] = useState<AppFacet>("all");
  const facets = useMemo(() => appFacets(apps), [apps]);
  const rows = useMemo(
    (): AppRow[] =>
      apps.flatMap((app) =>
        inFacet(app, facet) ? [{ ...app, learned: learned?.get(app.id) ?? null }] : [],
      ),
    [apps, learned, facet],
  );

  const table = useAppTable({
    data: rows,
    columns: appColumns,
    getRowId: (app) => app.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const total = apps.length;

  return (
    <>
      <div className="flex flex-col gap-3">
        {total === 0 ? null : (
          <FacetChips label="Apps by state" facets={facets} value={facet} onChange={setFacet} />
        )}
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
      </div>
      <TableGroup>
        <DataTable
          table={table}
          empty={
            total === 0 ? (
              <EmptyApps />
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
                      setFacet("all");
                    }}
                  >
                    Clear filters
                  </Button>
                }
              />
            )
          }
        />
      </TableGroup>
      <AppDialog me={me} open={creating} onOpenChange={setCreating} mutations={mutations} />
    </>
  );
}

/** Which apps the list shows: all of them, or the ones that need the operator. */
type AppFacet = "all" | "pending" | "offline";

function inFacet(app: App, facet: AppFacet): boolean {
  if (facet === "pending") {
    return totalPendingRoutes(app.nodes) > 0;
  }

  if (facet === "offline") {
    return connectorMachineCounts(app.nodes).online === 0;
  }

  return true;
}

/**
 * The facet chips over the table, as the dashboard's Routes page counts its routes by kind: every
 * app, the ones with routes waiting for approval and the ones no connector serves right now.
 */
function appFacets(apps: readonly App[]): Facet<AppFacet>[] {
  return [
    { value: "all", label: "Apps", count: apps.length },
    {
      value: "pending",
      label: "Routes pending",
      count: apps.filter((app) => inFacet(app, "pending")).length,
    },
    {
      value: "offline",
      label: "No connector online",
      count: apps.filter((app) => inFacet(app, "offline")).length,
    },
  ];
}

/**
 * Nothing to list yet, so the panel explains the two halves an app needs: the definition the
 * operator writes here, and the machine that carries the tag and runs the connector.
 */
function EmptyApps(): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
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
        </div>
      }
    />
  );
}
