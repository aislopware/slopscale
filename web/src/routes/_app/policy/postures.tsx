import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, posturesQuery } from "~/api/queries.ts";
import { PosturesTab } from "~/components/access/postures-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { requireScope } from "~/lib/require-scope.ts";
import { textSearchSchema } from "~/lib/search-text.ts";

export const Route = createFileRoute("/_app/policy/postures")({
  validateSearch: textSearchSchema,
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(posturesQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: PosturesPage,
});

function PosturesPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { postures, geoIpAvailable } = useSuspenseQuery(posturesQuery).data;
  const { rules } = useSuspenseQuery(accessRulesQuery).data;

  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Postures"
        description="Conditions a machine must meet before a rule applies to it, such as a current client, a known serial number or a weekly window."
        meta={postures.length === 1 ? "1 posture" : `${postures.length} postures`}
      />
      <PosturesTab
        me={me}
        postures={postures}
        rules={rules}
        geoIpAvailable={geoIpAvailable}
        search={search.q ?? ""}
        onSearchChange={setSearch}
      />
    </>
  );
}
