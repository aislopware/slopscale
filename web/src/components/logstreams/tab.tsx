import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon, ShippingContainerIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { LogStream } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { logStreamColumns } from "~/components/logstreams/columns.tsx";
import { LogStreamDialog } from "~/components/logstreams/dialogs.tsx";
import { useLogStreamMutations } from "~/components/logstreams/mutations.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export function LogStreamsTab({
  me,
  streams,
  search,
  onSearchChange,
}: {
  readonly me: Me;
  readonly streams: readonly LogStream[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}): ReactElement {
  const query = useDeferredValue(search);
  const canEdit = can(me, "logs:configuration");
  const [creating, setCreating] = useState(false);
  const mutations = useLogStreamMutations();

  const table = useAppTable({
    data: streams,
    columns: logStreamColumns,
    getRowId: (stream) => stream.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const total = streams.length;
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
            New log stream
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, URL or destination"
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
                  icon={<ShippingContainerIcon size={emptyIconSize} />}
                  title="No log streams yet"
                  description="Ship the audit log to Splunk, Elasticsearch, Datadog, Axiom, Loki or any HTTP collector."
                  contents={
                    <Button
                      variant="primary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New log stream
                    </Button>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No log streams match"
                  description="No log stream matches this search."
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
                <TableFooter>{`Showing ${shown} of ${countStreams(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <LogStreamDialog open={creating} onOpenChange={setCreating} mutations={mutations} />
    </>
  );
}

export function countStreams(total: number): string {
  return total === 1 ? "1 log stream" : `${total} log streams`;
}
