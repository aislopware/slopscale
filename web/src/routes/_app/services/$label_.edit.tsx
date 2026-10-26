import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { servicesQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import { serviceLabel, servicePathLabel, serviceTitle } from "~/components/services/model.ts";
import { ServiceEditor } from "~/components/services/service-editor.tsx";
import { RouteNotFound } from "~/components/ui/error-page.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

// The trailing underscore keeps the edit page out of the service page's layout: it is a page of
// its own, with the service page as its parent in the trail.
export const Route = createFileRoute("/_app/services/$label_/edit")({
  loader: async ({ context }) => {
    await context.queryClient.query(servicesQuery);
  },
  component: EditServicePage,
});

function EditServicePage(): ReactElement {
  const { me } = Route.useRouteContext();
  const { label } = Route.useParams();
  const { services } = useSuspenseQuery(servicesQuery).data;
  const service = services.find(
    (candidate) => serviceLabel(candidate.name) === serviceLabel(label),
  );

  useBreadcrumb(
    service === undefined ? null : "Edit",
    service === undefined
      ? undefined
      : { label: serviceTitle(service), to: `/services/${servicePathLabel(service.name)}` },
  );

  if (service === undefined) {
    return <RouteNotFound />;
  }

  return (
    <FormLayout width="narrow">
      <PageHeader
        title={serviceTitle(service)}
        kicker="Edit service"
        description="The name and the addresses stay as they are; everything else is what clients are told about the service."
        meta={<span className="font-mono">{service.name}</span>}
      />
      <ServiceEditor key={service.name} service={service} canEdit={can(me, "services")} />
    </FormLayout>
  );
}
