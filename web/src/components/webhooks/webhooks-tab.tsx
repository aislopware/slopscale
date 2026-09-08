import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { PlusIcon, WebhooksLogoIcon } from "@phosphor-icons/react";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";

import type { Webhook } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { emptyIconSize, tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { useWebhookMutations } from "~/components/webhooks/mutations.ts";
import { webhookColumns } from "~/components/webhooks/webhook-columns.tsx";
import { WebhookDialog } from "~/components/webhooks/webhook-dialogs.tsx";

export function WebhooksTab({
  me,
  webhooks,
  eventTypes,
  search,
  onSearchChange,
}: {
  readonly me: Me;
  readonly webhooks: readonly Webhook[];
  readonly eventTypes: readonly string[];
  readonly search: string;
  readonly onSearchChange: (value: string) => void;
}): ReactElement {
  const query = useDeferredValue(search);
  const canEdit = can(me, "webhooks");
  const [creating, setCreating] = useState(false);
  const mutations = useWebhookMutations();

  const table = useAppTable({
    data: webhooks,
    columns: webhookColumns,
    getRowId: (webhook) => webhook.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "url", desc: false }] },
    meta: { me, eventTypes },
  });

  const total = webhooks.length;
  const shown = table.getRowModel().rows.length;

  return (
    <>
      <LayerCard className="overflow-hidden">
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
              New webhook
            </Button>
          }
        >
          <SearchInput
            value={search}
            placeholder="Search by URL, description, provider or event"
            onValueChange={onSearchChange}
          />
        </TableToolbar>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  icon={<WebhooksLogoIcon size={emptyIconSize} />}
                  title="No webhooks yet"
                  description="Post events to your own endpoint, a chat channel, Telegram, ntfy or email."
                  contents={
                    <Button
                      variant="primary"
                      disabled={!canEdit}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New webhook
                    </Button>
                  }
                />
              ) : (
                <Empty
                  className={tableEmptyClass}
                  size="sm"
                  title="No webhooks match"
                  description="No webhook matches this search."
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
                <TableFooter>{`Showing ${shown} of ${countWebhooks(total)}`}</TableFooter>
              )
            }
          />
        </table.AppTable>
      </LayerCard>
      <WebhookDialog
        eventTypes={eventTypes}
        open={creating}
        onOpenChange={setCreating}
        mutations={mutations}
      />
    </>
  );
}

export function countWebhooks(total: number): string {
  return total === 1 ? "1 webhook" : `${total} webhooks`;
}
