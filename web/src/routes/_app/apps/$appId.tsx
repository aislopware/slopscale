import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { appsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { AppEditor } from "~/components/apps/app-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/apps/$appId")({
  loader: async ({ context }) => {
    await context.queryClient.query(appsQuery);
  },
  component: AppPage,
});

function AppPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { appId } = Route.useParams();
  const { apps } = useSuspenseQuery(appsQuery).data;
  const app = apps.find((candidate) => candidate.id === appId);

  useBreadcrumb(app?.name ?? "App not found");

  if (app === undefined) {
    return (
      <FormLayout>
        <PageHeader title="App not found" />
        <Section title="App" bodyClassName="p-0">
          <SectionEmpty
            title="This app does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href="/apps" variant="secondary">
                Back to apps
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
        title={app.name}
        kicker="App"
        {...(app.description === "" ? {} : { description: app.description })}
      />
      <AppEditor key={app.id} app={app} me={me} canEdit={can(me, "policy_file")} />
    </FormLayout>
  );
}
