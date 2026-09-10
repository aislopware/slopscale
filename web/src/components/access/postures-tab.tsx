import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { postureColumns } from "~/components/access/posture-columns.tsx";
import { PostureDialog } from "~/components/access/posture-dialogs.tsx";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";

export interface PosturesTabProps {
  readonly me: Me;
  readonly postures: readonly Posture[];
  readonly rules: readonly AccessRule[];
  readonly geoIpAvailable: boolean;
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}

/** Postures, the conditions a rule can require of the machine opening a connection. */
export function PosturesTab({
  me,
  postures,
  rules,
  geoIpAvailable,
  search,
  onSearchChange,
}: PosturesTabProps): ReactElement {
  const canEdit = can(me, "policy_file");
  const query = useDeferredValue(search);
  const [creating, setCreating] = useState(false);
  const mutations = useAccessMutations();

  const table = useAppTable({
    data: postures,
    columns: postureColumns,
    getRowId: (posture) => posture.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "name", desc: false }] },
    meta: { me, rules, geoIpAvailable },
  });

  const shown = table.getRowModel().rows.length;
  const total = postures.length;

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
            New posture
          </Button>
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by name, description or expression"
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
                  title="No postures yet"
                  description="A posture names conditions a machine must meet. Attach it to a rule to require it of the sources."
                  contents={
                    <Button
                      variant="secondary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New posture
                    </Button>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No postures match"
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
                <TableFooter>{`Showing ${shown} of ${total === 1 ? "1 posture" : `${total} postures`}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </Frame>
      <PostureDialog
        geoIpAvailable={geoIpAvailable}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}
