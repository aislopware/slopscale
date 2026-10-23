import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { can } from "~/auth/me.ts";
import { AddUserPage } from "~/components/users/user-page.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/users_/new")({
  component: NewUserPage,
});

function NewUserPage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Add user");

  return <AddUserPage canEdit={can(me, "users")} />;
}
