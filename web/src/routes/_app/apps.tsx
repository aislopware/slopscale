import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { appsQuery } from "~/api/queries.ts";
import { AppsTab } from "~/components/apps/apps-tab.tsx";
import { countApps, totalPendingRoutes } from "~/components/apps/model.ts";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/apps")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await context.queryClient.query(appsQuery);
  },
  component: AppsPage,
});

function AppsPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { apps } = useSuspenseQuery(appsQuery).data;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  const pending = apps.reduce((sum, app) => sum + totalPendingRoutes(app.nodes), 0);
  const meta =
    pending === 0
      ? countApps(apps.length)
      : `${countApps(apps.length)} · ${pending} routes pending`;

  return (
    <>
      <PageHeader
        title="Apps"
        description="Domains reached through app connectors: the connectors resolve them, advertise a route for every address they learn and forward the traffic."
        meta={meta}
      />
      <AppsTab me={me} apps={apps} search={search.q ?? ""} onSearchChange={setSearch} />
    </>
  );
}
