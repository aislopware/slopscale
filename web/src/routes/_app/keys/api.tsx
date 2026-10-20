import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { apiKeysQuery } from "~/api/queries.ts";
import { CreateKeyLink } from "~/components/keys/create-link.tsx";
import { ApiPanel } from "~/components/keys/panels.tsx";
import { keyControls, keysSearchSchema } from "~/components/keys/search.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/keys/api")({
  validateSearch: keysSearchSchema,
  loaderDeps: () => ({}),
  loader: async ({ context }) => {
    await context.queryClient.query(apiKeysQuery);
  },
  component: ApiKeysPage,
});

function ApiKeysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const controls = keyControls(search, (next, replace) => {
    void navigate({ search: () => next, replace });
  });

  return (
    <>
      <PageHeader
        title="API keys"
        docs="ref/api"
        description="Authenticate this console and automation against the v1 API. Rotating a key gives it a new secret, same id and scopes."
        // Every caller may mint keys of their own, so the action is never off.
        actions={<CreateKeyLink to="/keys/api/new" label="Create API key" allowed />}
      />
      <ApiPanel me={me} controls={controls} />
    </>
  );
}
