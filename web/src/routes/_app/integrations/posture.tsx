import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { postureIntegrationsQuery, postureProvidersQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { countIntegrations, integrationStatus } from "~/components/posture-integrations/model.ts";
import { PostureIntegrationsTab } from "~/components/posture-integrations/posture-integrations-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/integrations/posture")({
  validateSearch: textSearchSchema,
  beforeLoad: ({ context }) => {
    if (!can(context.me, "devices:posture_attributes:read")) {
      throw redirect({
        to: can(context.me, "webhooks:read")
          ? "/integrations/webhooks"
          : "/integrations/log-streams",
        replace: true,
      });
    }
  },
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(postureIntegrationsQuery),
      context.queryClient.query(postureProvidersQuery),
    ]);
  },
  component: PostureIntegrationsPage,
});

function PostureIntegrationsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { integrations } = useSuspenseQuery(postureIntegrationsQuery).data;
  const { providers } = useSuspenseQuery(postureProvidersQuery).data;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  const failing = integrations.filter(
    (integration) => integrationStatus(integration) === "failed",
  ).length;
  const meta =
    failing > 0
      ? `${countIntegrations(integrations.length)} · ${failing} failing`
      : countIntegrations(integrations.length);

  return (
    <>
      <PageHeader
        title="Device posture"
        description="Endpoint security and device management services that sync machine attributes by serial number."
        meta={meta}
      />
      <PostureIntegrationsTab
        me={me}
        integrations={integrations}
        providers={providers}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
