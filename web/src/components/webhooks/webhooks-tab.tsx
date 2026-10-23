import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { PlusIcon } from "@phosphor-icons/react";
import { useDeferredValue } from "react";
import type { ReactElement } from "react";

import type { Webhook } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { tableEmptyClass } from "~/components/table/empty.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableGroup, TableToolbar } from "~/components/table/toolbar.tsx";
import { webhookColumns } from "~/components/webhooks/webhook-columns.tsx";

const newWebhookPath = "/integrations/webhooks/new";

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

  const table = useAppTable({
    data: webhooks,
    columns: webhookColumns,
    getRowId: (webhook) => webhook.id,
    state: { globalFilter: query },
    initialState: { sorting: [{ id: "url", desc: false }] },
    meta: { me, eventTypes },
  });

  const total = webhooks.length;

  return (
    <>
      <TableToolbar
        actions={
          canEdit ? (
            <LinkButton href={newWebhookPath} variant="primary" icon={PlusIcon}>
              New webhook
            </LinkButton>
          ) : (
            <Button variant="primary" icon={PlusIcon} disabled>
              New webhook
            </Button>
          )
        }
      >
        <SearchInput
          value={search}
          placeholder="Search by URL, description, provider or event"
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
                title="No webhooks yet"
                description="Post events to your own endpoint, a chat channel, Telegram, ntfy or email."
                contents={
                  canEdit ? (
                    <LinkButton href={newWebhookPath} variant="secondary" icon={PlusIcon}>
                      Add your first webhook
                    </LinkButton>
                  ) : undefined
                }
              />
            ) : (
              <Empty
                className={tableEmptyClass}
                size="sm"
                title="No webhooks match"
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

export function countWebhooks(total: number): string {
  return total === 1 ? "1 webhook" : `${total} webhooks`;
}
