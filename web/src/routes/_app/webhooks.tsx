import { Button } from "@cloudflare/kumo/components/button";
import { Empty } from "@cloudflare/kumo/components/empty";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { PlusIcon, WebhooksLogoIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useDeferredValue, useState } from "react";
import type { ReactElement } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { webhookEventTypesQuery, webhooksQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter, TableToolbar } from "~/components/table/toolbar.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { deliveryState } from "~/components/webhooks/model.ts";
import { useWebhookMutations } from "~/components/webhooks/mutations.ts";
import { webhookColumns } from "~/components/webhooks/webhook-columns.tsx";
import { WebhookDialog } from "~/components/webhooks/webhook-dialogs.tsx";

const emptyClass = "border-none bg-kumo-base [&>h2]:text-base";
const emptyIconSize = 32;

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

const optionalText = optional(pipe(unknown(), transform(toText)));
const searchSchema = object({ q: optionalText });

export const Route = createFileRoute("/_app/webhooks")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(webhooksQuery),
      context.queryClient.query(webhookEventTypesQuery),
    ]);
  },
  component: WebhooksPage,
});

function WebhooksPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { webhooks } = useSuspenseQuery(webhooksQuery).data;
  const { types: eventTypes } = useSuspenseQuery(webhookEventTypesQuery).data;
  const text = search.q ?? "";
  const query = useDeferredValue(text);
  const canEdit = can(me, "webhooks");
  const [creating, setCreating] = useState(false);
  const mutations = useWebhookMutations();

  const setSearch = (value: string): void => {
    void navigate({
      search: () => ({ q: value === "" ? undefined : value }),
      replace: true,
    });
  };

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
  const failing = webhooks.filter((webhook) => deliveryState(webhook) === "failed").length;

  return (
    <>
      <PageHeader
        title="Webhooks"
        description="Endpoints the server posts events to: machines joining and leaving, users changing, the policy changing. Deliveries are signed with each endpoint's secret in the Tailscale format."
        meta={describe(total, failing)}
      />
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
            value={text}
            placeholder="Search by URL, description, provider or event"
            onValueChange={setSearch}
          />
        </TableToolbar>
        <table.AppTable>
          <DataTable
            empty={
              total === 0 ? (
                <Empty
                  className={emptyClass}
                  size="sm"
                  icon={<WebhooksLogoIcon size={emptyIconSize} />}
                  title="No webhooks yet"
                  description="Post events to your own endpoint or to a Slack, Mattermost, Google Chat or Discord channel."
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
                  className={emptyClass}
                  size="sm"
                  title="No webhooks match"
                  description="No webhook matches this search."
                  contents={
                    <Button
                      variant="secondary"
                      onClick={() => {
                        setSearch("");
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

function countWebhooks(total: number): string {
  return total === 1 ? "1 webhook" : `${total} webhooks`;
}

function describe(total: number, failing: number): string {
  const totalText = countWebhooks(total);

  return failing === 0 ? totalText : `${totalText} · ${failing} failing`;
}
