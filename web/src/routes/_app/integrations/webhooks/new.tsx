import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { webhookEventTypesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { FormBar, FormLayout, FormSection } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { WebhookEditor, webhooksPath } from "~/components/webhooks/webhook-editor.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/integrations/webhooks/new")({
  // A reader who may not add webhooks has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "webhooks")) {
      throw redirect({ to: "/integrations/webhooks", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(webhookEventTypesQuery);
  },
  component: NewWebhookPage,
});

/** Creating a webhook, then its signing secret on the same page, shown this once. */
function NewWebhookPage(): ReactElement {
  const { types: eventTypes } = useSuspenseQuery(webhookEventTypesQuery).data;
  const [secret, setSecret] = useState<string | null>(null);

  useBreadcrumb("New webhook");

  return secret === null ? (
    <FormLayout>
      <PageHeader
        title="New webhook"
        docs="ref/webhooks"
        description="An endpoint the server posts events to as they happen. Generic endpoints get the signed JSON array, the rest get the message alone."
      />
      <WebhookEditor eventTypes={eventTypes} canEdit onCreated={setSecret} />
    </FormLayout>
  ) : (
    <FormLayout>
      <PageHeader
        title="Webhook created"
        docs="ref/webhooks"
        description="Copy the signing secret before you leave this page. It is not shown again."
      />
      <FormSection title="Signing secret">
        <RevealedSecret
          noun="secret"
          value={secret}
          note="The secret signs every delivery so the receiver can verify it. It is shown only once."
        />
      </FormSection>
      <FormBar>
        <LinkButton href={webhooksPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}
