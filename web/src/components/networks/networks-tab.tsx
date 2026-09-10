import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useMemo, useState } from "react";
import type { ReactElement } from "react";

import type { Group, Network, Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { networkColumns, toNetworkRows } from "~/components/networks/network-columns.tsx";
import { NetworkDialog } from "~/components/networks/network-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface NetworksTabProps {
  readonly me: Me;
  readonly networks: readonly Network[];
  /** Whether the tailnet has a packet filter, so a network's protocol and ports take effect. */
  readonly enforcing: boolean;
  readonly groups: readonly Group[];
  readonly nodes: readonly Node[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Networks: prefixes, the machines that route them, and the groups that receive them. */
export function NetworksTab({
  me,
  networks,
  enforcing,
  groups,
  nodes,
  search,
  onSearchChange,
}: NetworksTabProps): ReactElement {
  const canEdit = can(me, "devices:routes");
  const query = useDeferredValue(search);
  const [creating, setCreating] = useState(false);
  const mutations = useNetworkMutations();
  const rows = useMemo(() => toNetworkRows(networks, groups), [networks, groups]);

  const table = useAppTable({
    data: rows,
    columns: networkColumns,
    getRowId: (network) => network.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, groups, nodes, networks, enforcing },
  });

  const total = networks.length;
  const shown = table.getRowModel().rows.length;
  const enabled = networks.filter((network) => network.enabled).length;

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
            New network
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, prefix, router or group"
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
                  title="No networks"
                  description="A network approves a subnet or exit node on its routers and hands the routes only to the groups you pick."
                  contents={
                    <Button
                      variant="secondary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New network
                    </Button>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No networks match"
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
                <TableFooter>{`Showing ${shown} of ${countNetworks(total)} · ${enabled} enabled`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <NetworkDialog
        groups={groups}
        nodes={nodes}
        enforcing={enforcing}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}

function countNetworks(total: number): string {
  return total === 1 ? "1 network" : `${total} networks`;
}
