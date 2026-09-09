import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { preAuthKeysQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PreAuthPanel } from "~/components/keys/panels.tsx";
import { keyControls, keysSearchSchema } from "~/components/keys/search.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/keys/pre-auth")({
  validateSearch: keysSearchSchema,
  loaderDeps: () => ({}),
  beforeLoad: ({ context }) => {
    if (!can(context.me, "auth_keys:read")) {
      throw redirect({ to: "/keys/api", replace: true });
    }
  },
  loader: async ({ context }) => {
    await context.queryClient.query(preAuthKeysQuery);
  },
  component: PreAuthKeysPage,
});

function PreAuthKeysPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const controls = keyControls(search, (next, replace) => {
    void navigate({ search: () => next, replace });
  });

  return (
    <>
      <PageHeader
        title="Pre-auth keys"
        description="Register machines without a login. A key sets the user or tags the machine joins as."
      />
      <PreAuthPanel me={me} controls={controls} />
    </>
  );
}
