import { createFileRoute, redirect } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { LogStreamEditor } from "~/components/logstreams/editor.tsx";
import { FormLayout } from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/integrations/log-streams/new")({
  // A reader who may not add streams has nothing to do here; the list says why its button is off.
  beforeLoad: ({ context }) => {
    if (!can(context.me, "logs:configuration")) {
      throw redirect({ to: "/integrations/log-streams", replace: true });
    }
  },
  component: NewLogStreamPage,
});

function NewLogStreamPage(): ReactElement {
  useBreadcrumb("New log stream");

  return (
    <FormLayout>
      <PageHeader
        title="New log stream"
        description="Ships every audit log entry to a SIEM or log store in batches, in the shape it expects."
      />
      <LogStreamEditor canEdit />
    </FormLayout>
  );
}
