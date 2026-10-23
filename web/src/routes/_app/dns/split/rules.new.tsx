import { useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { DnsRuleEditor } from "~/components/dns/rule-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/dns/split/rules/new")({
  // A reader who may not change DNS has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "dns")) {
      throw redirect({ to: "/dns/split", replace: true });
    }
  },
  loader: async ({ context }) => {
    if (can(context.me, "policy_file:read")) {
      await context.queryClient.query(groupsQuery);
    }
  },
  component: NewDnsRulePage,
});

function NewDnsRulePage(): ReactElement {
  const { me } = Route.useRouteContext();
  // The group list needs its own scope; without it the picker has nothing to offer.
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });

  useBreadcrumb("New DNS rule");

  return (
    <FormLayout>
      <PageHeader
        title="New DNS rule"
        docs="ref/dns"
        description="Split DNS that only the machines in the groups you pick receive."
      />
      <DnsRuleEditor groups={groups.data?.groups ?? []} canEdit={can(me, "dns")} />
    </FormLayout>
  );
}
