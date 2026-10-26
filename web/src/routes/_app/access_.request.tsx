import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRequestOptionsQuery } from "~/api/queries.ts";
import { askReason, RequestAccessForm } from "~/components/access/request-access-page.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/access_/request")({
  loader: async ({ context }) => {
    await context.queryClient.query(accessRequestOptionsQuery);
  },
  component: RequestAccessPage,
});

function RequestAccessPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const options = useSuspenseQuery(accessRequestOptionsQuery).data;
  const reason = askReason(me.user !== undefined, options.groups.length > 0);

  useBreadcrumb("Request access");

  return (
    <FormLayout>
      <PageHeader
        title="Request access"
        description="Ask to join a group for a while. An approver decides, and the access ends on its own."
      />
      {reason === undefined ? (
        <RequestAccessForm options={options} />
      ) : (
        <Section title="Request" bodyClassName="p-0">
          <SectionEmpty
            title={reason}
            description={
              me.user === undefined
                ? "Sign in as a user to ask for access."
                : "An administrator can mark a group requestable under Access controls."
            }
            contents={
              <LinkButton href="/access" variant="secondary">
                Back to my access
              </LinkButton>
            }
          />
        </Section>
      )}
    </FormLayout>
  );
}
