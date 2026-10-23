import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { webhookEventTypesQuery, webhooksQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { providerLabel, urlHost } from "~/components/webhooks/model.ts";
import { WebhookEditor, webhooksPath } from "~/components/webhooks/webhook-editor.tsx";
import { WebhookPageActions } from "~/components/webhooks/webhook-menu.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/integrations/webhooks/$webhookId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(webhooksQuery),
      context.queryClient.query(webhookEventTypesQuery),
    ]);
  },
  component: WebhookPage,
});

function WebhookPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { webhookId } = Route.useParams();
  const { webhooks } = useSuspenseQuery(webhooksQuery).data;
  const { types: eventTypes } = useSuspenseQuery(webhookEventTypesQuery).data;
  const webhook = webhooks.find((candidate) => candidate.id === webhookId);
  const canEdit = can(me, "webhooks");
  // A webhook has no name; its description stands in, or else where it posts.
  let title = "Webhook not found";

  if (webhook !== undefined) {
    title = webhook.description === "" ? urlHost(webhook.url) : webhook.description;
  }

  useBreadcrumb(title);

  if (webhook === undefined) {
    return (
      <FormLayout>
        <PageHeader title={title} docs="ref/webhooks" />
        <Section title="Webhook" bodyClassName="p-0">
          <SectionEmpty
            title="This webhook does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href={webhooksPath} variant="secondary">
                Back to webhooks
              </LinkButton>
            }
          />
        </Section>
      </FormLayout>
    );
  }

  return (
    <FormLayout>
      <PageHeader
        title={title}
        kicker="Webhook"
        docs="ref/webhooks"
        meta={providerLabel(webhook.providerType)}
        actions={<WebhookPageActions webhook={webhook} canEdit={canEdit} />}
      />
      <WebhookEditor key={webhook.id} webhook={webhook} eventTypes={eventTypes} canEdit={canEdit} />
    </FormLayout>
  );
}
