import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { accessRulesQuery, posturesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { PostureEditor } from "~/components/access/posture-editor.tsx";
import { rulesUsingPosture } from "~/components/access/posture-model.ts";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/policy/postures/$postureId")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(posturesQuery),
      context.queryClient.query(accessRulesQuery),
    ]);
  },
  component: PosturePage,
});

function countRules(count: number): string {
  if (count === 0) {
    return "No rule requires it";
  }

  return count === 1 ? "Required by 1 rule" : `Required by ${count} rules`;
}

function PosturePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { postureId } = Route.useParams();
  const { postures, geoIpAvailable } = useSuspenseQuery(posturesQuery).data;
  const { rules } = useSuspenseQuery(accessRulesQuery).data;
  const posture = postures.find((candidate) => candidate.id === postureId);

  useBreadcrumb(posture?.name ?? "Posture not found");

  if (posture === undefined) {
    return (
      <FormLayout>
        <PageHeader title="Posture not found" docs="ref/device-trust" />
        <Section title="Posture" bodyClassName="p-0">
          <SectionEmpty
            title="This posture does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href="/policy/postures" variant="secondary">
                Back to postures
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
        title={posture.name}
        kicker="Posture"
        docs="ref/device-trust"
        {...(posture.description === "" ? {} : { description: posture.description })}
        meta={countRules(rulesUsingPosture(rules, posture).length)}
      />
      <PostureEditor
        key={posture.id}
        posture={posture}
        rules={rules}
        geoIpAvailable={geoIpAvailable}
        canEdit={can(me, "policy_file")}
      />
    </FormLayout>
  );
}
