import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon, ShieldCheckIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { PostureIntegration, PostureProvider } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { countIntegrations } from "~/components/posture-integrations/model.ts";
import { usePostureIntegrationMutations } from "~/components/posture-integrations/mutations.ts";
import { postureIntegrationColumns } from "~/components/posture-integrations/posture-integration-columns.tsx";
import { PostureIntegrationDialog } from "~/components/posture-integrations/posture-integration-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

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
            New posture integration
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, provider or prefix"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  icon={<ShieldCheckIcon size={emptyIconSize} />}
                  title="No posture integrations yet"
                  description="Connect endpoint security or device management services to check machine attributes in postures."
                  contents={
                    <Button
                      variant="primary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New posture integration
                    </Button>
                  }
                />
              ) : (
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
              )
            }
            footer={
              total === 0 ? undefined : (
                <TableFooter>{`Showing ${shown} of ${countIntegrations(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <PostureIntegrationDialog
        providers={providers}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}
