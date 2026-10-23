import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object } from "valibot";

import { postureProvidersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PostureIntegrationEditor } from "~/components/posture-integrations/posture-integration-editor.tsx";
import { ProviderTiles } from "~/components/posture-integrations/provider-picker.tsx";
import { FormLayout, FormSection } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";
import { optionalText } from "~/lib/search-text.ts";

const newPath = "/integrations/posture/new";

export const Route = createFileRoute("/_app/integrations/posture/new")({
  // The provider is chosen first, from tiles on this same page, and then carried in the address.
  validateSearch: object({ provider: optionalText }),
  // A reader who may not add integrations has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "devices:posture_attributes")) {
      throw redirect({ to: "/integrations/posture", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(postureProvidersQuery);
  },
  component: NewPostureIntegrationPage,
});

/**
 * A new posture integration, as the dashboard adds a posture check: the providers as tiles first,
 * then the chosen provider's form at the same address with the provider in the search.
 */
function NewPostureIntegrationPage(): ReactElement {
  const { provider: chosen } = Route.useSearch();
  const { providers } = useSuspenseQuery(postureProvidersQuery).data;
  const provider = providers.find((candidate) => candidate.provider === chosen);

  useBreadcrumb(
    provider?.label ?? "New posture integration",
    provider === undefined ? undefined : { label: "New posture integration", to: newPath },
  );

  if (provider === undefined) {
    return (
      <FormLayout>
        <PageHeader
          title="New posture integration"
          docs="ref/device-trust"
          description="Pick the endpoint security or device management service to connect. Its attributes sync to machines by serial number, ready for postures to check."
        />
        <FormSection title="Select a provider">
          <ProviderTiles providers={providers} disabled={false} />
        </FormSection>
      </FormLayout>
    );
  }

  return (
    <FormLayout>
      <PageHeader
        title={`Connect ${provider.label}`}
        docs="ref/device-trust"
        description={`Syncs ${provider.label} attributes to machines by serial number, ready for postures to check.`}
      />
      <PostureIntegrationEditor key={provider.provider} provider={provider} canEdit />
    </FormLayout>
  );
}
