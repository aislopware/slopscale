import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { logStreamsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { LogStreamEditor, logStreamsPath } from "~/components/logstreams/editor.tsx";
import { SendTestEntryButton } from "~/components/logstreams/menu.tsx";
import { countersLabel, destinationLabel } from "~/components/logstreams/model.ts";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/integrations/log-streams/$streamId")({
  loader: async ({ context }) => {
    await context.queryClient.query(logStreamsQuery);
  },
  component: LogStreamPage,
});

function LogStreamPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { streamId } = Route.useParams();
  const { logStreams } = useSuspenseQuery(logStreamsQuery).data;
  const stream = logStreams.find((candidate) => candidate.id === streamId);
  const canEdit = can(me, "logs:configuration");

  useBreadcrumb(stream?.name ?? "Log stream not found");

  if (stream === undefined) {
    return (
      <FormLayout>
        <PageHeader title="Log stream not found" />
        <Section title="Log stream" bodyClassName="p-0">
          <SectionEmpty
            title="This log stream does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href={logStreamsPath} variant="secondary">
                Back to log streams
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
        title={stream.name}
        kicker="Log stream"
        meta={`${destinationLabel(stream.destination)} · ${countersLabel(stream)}`}
        actions={<SendTestEntryButton stream={stream} canEdit={canEdit} />}
      />
      <LogStreamEditor key={stream.id} stream={stream} canEdit={canEdit} />
    </FormLayout>
  );
}
