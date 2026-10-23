import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";

import type { LogStream } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { logStreamColumns } from "~/components/logstreams/columns.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";

const newStreamPath = "/integrations/log-streams/new";

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

  const table = useAppTable({
    data: streams,
    columns: logStreamColumns,
    getRowId: (stream) => stream.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me },
  });

  const total = streams.length;

  return (
    <>
      <TableToolbar
        actions={
          canEdit ? (
            <LinkButton href={newStreamPath} variant="primary" icon={PlusIcon}>
              New log stream
            </LinkButton>
          ) : (
            <Button variant="primary" icon={PlusIcon} disabled>
              New log stream
            </Button>
          )
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, URL or destination"
          onValueChange={onSearchChange}
        />
      </TableToolbar>
      <TableGroup>
        <DataTable
          table={table}
          empty={
            total === 0 ? (
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No log streams yet"
                description="Ship the audit log to Splunk, Elasticsearch, Datadog, Axiom, Loki or any HTTP collector."
                contents={
                  canEdit ? (
                    <LinkButton href={newStreamPath} variant="secondary" icon={PlusIcon}>
                      Add your first log stream
                    </LinkButton>
                  ) : undefined
                }
              />
            ) : (
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No log streams match"
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
        />
      </TableGroup>
    </>
  );
}

export function countStreams(total: number): string {
  return total === 1 ? "1 log stream" : `${total} log streams`;
}
