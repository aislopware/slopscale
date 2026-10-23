import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { postureIntegrationsQuery, postureProvidersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import {
  integrationStatus,
  providerLabel,
  statusLabel,
} from "~/components/posture-integrations/model.ts";
import {
  PostureIntegrationEditor,
  postureIntegrationsPath,
} from "~/components/posture-integrations/posture-integration-editor.tsx";
import { SyncNowButton } from "~/components/posture-integrations/posture-integration-menu.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/integrations/posture/$integrationId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(postureIntegrationsQuery),
      context.queryClient.query(postureProvidersQuery),
    ]);
  },
  component: PostureIntegrationPage,
});

function PostureIntegrationPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { integrationId } = Route.useParams();
  const { integrations } = useSuspenseQuery(postureIntegrationsQuery).data;
  const { providers } = useSuspenseQuery(postureProvidersQuery).data;
  const integration = integrations.find((candidate) => candidate.id === integrationId);
  const provider = providers.find((candidate) => candidate.provider === integration?.provider);
  const canEdit = can(me, "devices:posture_attributes");

  useBreadcrumb(integration?.name ?? "Integration not found");

  // An integration whose provider this server no longer offers cannot be edited either.
  if (integration === undefined || provider === undefined) {
    return (
      <FormLayout>
        <PageHeader title="Integration not found" docs="ref/device-trust" />
        <Section title="Posture integration" bodyClassName="p-0">
          <SectionEmpty
            title="This integration does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href={postureIntegrationsPath} variant="secondary">
                Back to device posture
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
        title={integration.name}
        kicker="Posture integration"
        docs="ref/device-trust"
        meta={`${providerLabel(providers, integration.provider)} · ${statusLabel(integrationStatus(integration))} · ${integration.lastMatched.toLocaleString()} machines matched`}
        actions={<SyncNowButton integration={integration} canEdit={canEdit} />}
      />
      <PostureIntegrationEditor
        key={integration.id}
        integration={integration}
        provider={provider}
        canEdit={canEdit}
      />
    </FormLayout>
  );
}
