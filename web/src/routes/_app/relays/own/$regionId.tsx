import { LinkButton } from "@cloudflare/kumo/components/button";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { derpQuery } from "~/api/queries.ts";
import type { DerpCustomRegion } from "~/api/schema.gen.ts";
import { can } from "~/auth/me.ts";
import { RegionEditor } from "~/components/derp/region-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/relays/own/$regionId")({
  loader: async ({ context }) => {
    await context.queryClient.query(derpQuery);
  },
  component: RegionPage,
});

/** The region's name, or its code when it has none, as the map falls back to it. */
function regionTitle(region: DerpCustomRegion): string {
  return region.name === undefined || region.name === "" ? region.code : region.name;
}

function RegionPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { regionId } = Route.useParams();
  const { derp } = useSuspenseQuery(derpQuery).data;
  const found = derp.effective.regions.find((candidate) => String(candidate.id) === regionId);
  // The region as the page last found it. A delete, or a save that changes the id, takes it out
  // of the settings before the editor has gone back to the list; keeping it keeps the editor, and
  // the callbacks that take it there, mounted.
  const [kept, setKept] = useState({ id: regionId, region: found });

  if (found !== undefined && (found !== kept.region || regionId !== kept.id)) {
    setKept({ id: regionId, region: found });
  }

  const region = found ?? (kept.id === regionId ? kept.region : undefined);
  const title = region === undefined ? "Region not found" : regionTitle(region);

  useBreadcrumb(title);

  if (region === undefined) {
    return (
      <FormLayout>
        <PageHeader title={title} />
        <Section title="Region" bodyClassName="p-0">
          <SectionEmpty
            title="This region does not exist"
            description="It may have been deleted since the link was made."
            contents={
              <LinkButton href="/relays/own" variant="secondary">
                Back to your relays
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
        title={title}
        kicker={
          title === region.code
            ? `Region ${String(region.id)}`
            : `Region ${String(region.id)} · ${region.code}`
        }
      />
      <RegionEditor
        key={region.id}
        region={region}
        derp={derp}
        canEdit={can(me, "feature_settings")}
      />
    </FormLayout>
  );
}
