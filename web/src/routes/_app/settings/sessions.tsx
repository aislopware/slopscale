import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { SessionSection } from "~/components/settings/session-section.tsx";
import { ConsoleSessionsSection } from "~/components/settings/sessions-section.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

export const Route = createFileRoute("/_app/settings/sessions")({
  component: SessionsSettingsPage,
});

function SessionsSettingsPage(): ReactElement {
  const { me } = Route.useRouteContext();

  return (
    <>
      <PageHeader
        title="Sessions"
        description="The credential this browser holds, and every console session on the server."
      />
      <div className="flex flex-col gap-6">
        <SessionSection me={me} />
        <ConsoleSessionsSection me={me} />
      </div>
    </>
  );
}
