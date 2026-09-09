import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { servicesQuery } from "~/api/queries.ts";
import { reachableCount } from "~/components/services/model.ts";
import { ServicesTab } from "~/components/services/services-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/services/")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("services:read"),
  loader: async ({ context }) => {
    await context.queryClient.query(servicesQuery);
  },
  component: ServicesPage,
});

function ServicesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { services } = useSuspenseQuery(servicesQuery).data;
  const reachable = reachableCount(services);

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Services"
        description="Names with tailnet addresses of their own that tagged machines host, so a client reaches one by name however it moves."
        meta={reachable === 1 ? "1 service reachable" : `${reachable} services reachable`}
      />
      <ServicesTab me={me} services={services} search={search.q ?? ""} onSearchChange={setSearch} />
    </>
  );
}
