import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { HardDrivesIcon, PlusIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { Service } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { toServiceRows } from "~/components/services/model.ts";
import { useServiceMutations } from "~/components/services/mutations.ts";
import { serviceColumns } from "~/components/services/service-columns.tsx";
import { ServiceDialog } from "~/components/services/service-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { CommandBox } from "~/components/ui/command-text.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface ServicesTabProps {
  readonly me: Me;
  readonly services: readonly Service[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** The tailnet's services: what each one is called, where it answers and which machines host it. */
export function ServicesTab({
  me,
  services,
  search,
  onSearchChange,
}: ServicesTabProps): ReactElement {
  const canEdit = can(me, "services");
  const query = useDeferredValue(search);
  const navigate = useNavigate();
  const [creating, setCreating] = useState(false);
  const mutations = useServiceMutations();
  const rows = useMemo(() => toServiceRows(services), [services]);

  const table = useAppTable({
    data: rows,
    columns: serviceColumns,
    getRowId: (service) => service.label,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const total = services.length;
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
            New service
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, address, port or host"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <Frame>
        <table.AppTable>
          <DataTable
            onRowClick={(label) => {
              void navigate({ to: "/services/$label", params: { label } });
            }}
            empty={
              total === 0 ? (
                <ServicesEmpty
                  canEdit={canEdit}
                  onCreate={() => {
                    setCreating(true);
                  }}
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No services match"
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
                <TableFooter>{`Showing ${shown} of ${countServices(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <ServiceDialog
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
        onCreated={(label) => {
          void navigate({ to: "/services/$label", params: { label } });
        }}
      />
    </>
  );
}

/** The first service takes two commands on the machine as well, so the empty state carries them. */
function ServicesEmpty({
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
      icon={<HardDrivesIcon size={emptyIconSize} />}
      title="No services"
      description="A service is a name and a pair of addresses of its own that tagged machines host, so clients reach it by name however it moves."
      contents={
        <div className="flex w-full max-w-md flex-col gap-3 text-left">
          <CommandBox
            size="sm"
            command="tailscale serve --service=svc:web --https=443 localhost:8080"
          />
          <CommandBox size="sm" command="tailscale serve advertise svc:web" />
          <p className="text-kumo-subtle">
            Create the service here, run those on a tagged machine, then approve the machine to host
            it.
          </p>
          <Button
            variant="secondary"
            icon={PlusIcon}
            disabled={!canEdit}
            className="self-start"
            onClick={onCreate}
          >
            New service
          </Button>
        </div>
      }
    />
  );
}

function countServices(total: number): string {
  return total === 1 ? "1 service" : `${total} services`;
}
