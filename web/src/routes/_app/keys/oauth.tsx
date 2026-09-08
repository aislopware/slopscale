import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { oauthClientsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { OAuthPanel } from "~/components/keys/panels.tsx";
import { keyControls, keysSearchSchema } from "~/components/keys/search.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/keys/oauth")({
  validateSearch: keysSearchSchema,
  loaderDeps: () => ({}),
  beforeLoad: ({ context }) => {
    if (!can(context.me, "oauth_keys:read")) {
      throw redirect({ to: "/keys/api", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(oauthClientsQuery);
  },
  component: OAuthClientsPage,
});

function OAuthClientsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const controls = keyControls(search, (next, replace) => {
    void navigate({ search: () => next, replace });
  });

  return (
    <>
      <PageHeader
        title="OAuth clients"
        description="Client credentials for the v2 API, each with the scopes and tags it may act with. A client secret is shown once."
      />
      <OAuthPanel me={me} controls={controls} />
    </>
  );
}
