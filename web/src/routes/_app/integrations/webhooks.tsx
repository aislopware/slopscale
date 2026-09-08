import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { webhookEventTypesQuery, webhooksQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { deliveryState } from "~/components/webhooks/model.ts";
import { WebhooksTab, countWebhooks } from "~/components/webhooks/webhooks-tab.tsx";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/integrations/webhooks")({
  validateSearch: textSearchSchema,
  beforeLoad: ({ context }) => {
    if (!can(context.me, "webhooks:read")) {
      throw redirect({ to: "/integrations/log-streams", replace: true });
    }
  },
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

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  const failing = webhooks.filter((webhook) => deliveryState(webhook) === "failed").length;
  const meta =
    failing > 0
      ? `${countWebhooks(webhooks.length)} · ${failing} failing`
      : countWebhooks(webhooks.length);

  return (
    <>
      <PageHeader
        title="Webhooks"
        description="Endpoints, chat channels and inboxes that receive signed events as they happen."
        meta={meta}
      />
      <WebhooksTab
        me={me}
        webhooks={webhooks}
        eventTypes={eventTypes}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
