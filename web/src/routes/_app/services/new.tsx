import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { ServiceEditor } from "~/components/services/service-editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/services/new")({
  // A reader who may not create services has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "services")) {
      throw redirect({ to: "/services", replace: true });
    }
  },
  component: NewServicePage,
});

function NewServicePage(): ReactElement {
  useBreadcrumb("New service");

  return (
    <FormLayout width="narrow">
      <PageHeader
        title="New service"
        docs="ref/services"
        description="A name of its own with a pair of tailnet addresses, hosted by the tagged machines you approve."
      />
      <ServiceEditor canEdit />
    </FormLayout>
  );
}
