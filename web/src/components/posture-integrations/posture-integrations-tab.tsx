import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { usePostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { postureIntegrationColumns } from "~/components/posture-integrations/posture-integration-columns.tsx";
import { PostureIntegrationDialog } from "~/components/posture-integrations/posture-integration-dialogs.tsx";
import { ProviderPicker } from "~/components/posture-integrations/provider-picker.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";

export function PostureIntegrationsTab({
  me,
  integrations,
  providers,
  search,
  onSearchChange,
}: {
  readonly me: Me;
  readonly integrations: readonly PostureIntegration[];
  readonly providers: readonly PostureProvider[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}): ReactElement {
  const query = useDeferredValue(search);
  const canEdit = can(me, "devices:posture_attributes");
  const [creating, setCreating] = useState(false);
  // The provider picked from the tiles, which the form starts with; empty when the button opened it.
  const [picked, setPicked] = useState("");
  const mutations = usePostureIntegrationMutations();

  const table = useAppTable({
    data: integrations,
    columns: postureIntegrationColumns,
    getRowId: (integration) => integration.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, postureProviders: providers },
  });

  const total = integrations.length;

  return (
    <>
      <TableToolbar
        actions={
          <Button
            variant="primary"
            icon={PlusIcon}
            disabled={!canEdit}
            onClick={() => {
              setPicked("");
              setCreating(true);
            }}
          >
            New posture integration
          </Button>
        }
      >
        {/* Nothing to search until something is connected; the tiles below are the list. */}
        {total === 0 ? null : (
          <SearchInput
            value={search}
            placeholder="Search by name, provider or prefix"
            onValueChange={onSearchChange}
          />
        )}
      </TableToolbar>
      {total === 0 ? (
        <ProviderPicker
          providers={providers}
          disabled={!canEdit}
          onPick={(provider) => {
            setPicked(provider);
            setCreating(true);
          }}
        />
      ) : (
        <TableGroup>
          <DataTable
            table={table}
            empty={
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No posture integrations match"
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
            }
          />
        </TableGroup>
      )}
      <PostureIntegrationDialog
        providers={providers}
        provider={picked}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}
