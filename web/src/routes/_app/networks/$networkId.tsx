import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { groupsQuery, networksQuery, nodesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { NetworkEditor } from "~/components/networks/network-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/networks/$networkId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(networksQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(nodesQuery),
    ]);
  },
  component: NetworkPage,
});

function NetworkPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { networkId } = Route.useParams();
  const { networks, enforcing } = useSuspenseQuery(networksQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { nodes } = useSuspenseQuery(nodesQuery).data;
  const network = networks.find((candidate) => candidate.id === networkId);

  useBreadcrumb(network?.name ?? "Network not found");

  if (network === undefined) {
    return (
      <FormLayout>
        <PageHeader title="Network not found" docs="ref/networks" />
        <Section title="Network" bodyClassName="p-0">
          <SectionEmpty
            title="This network does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href="/networks" variant="secondary">
                Back to networks
              </LinkButton>
            }
          />
        </Section>
      </FormLayout>
    );
  }

  return (
    <FormLayout>
      <PageHeader
        title={network.name}
        kicker="Network"
        docs="ref/networks"
        {...(network.description === "" ? {} : { description: network.description })}
      />
      <NetworkEditor
        key={network.id}
        network={network}
        groups={groups}
        nodes={nodes}
        enforcing={enforcing}
        canEdit={can(me, "devices:routes")}
      />
    </FormLayout>
  );
}
