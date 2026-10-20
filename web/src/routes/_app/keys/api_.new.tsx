import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { CreateApiKeyPage } from "~/components/keys/api-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/keys/api_/new")({
  component: NewApiKeyPage,
});

function NewApiKeyPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Create");

  return <CreateApiKeyPage me={me} />;
}
