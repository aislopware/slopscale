import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { apiKeysQuery } from "~/api/queries.ts";
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
        description="Authenticate this console and automation against the v1 API. A key is shown once; rotating it mints a new secret under the same id and scopes."
      />
      <ApiPanel me={me} controls={controls} />
    </>
  );
}
