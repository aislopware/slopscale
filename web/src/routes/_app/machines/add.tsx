import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import { AddMachineWizard } from "~/components/machines/add-wizard.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";
import { useBreadcrumb } from "~/lib/breadcrumbs.tsx";

export const Route = createFileRoute("/_app/machines/add")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(nodesQuery),
      canListUsers(context.me) ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: AddMachinePage,
});

/**
 * Adding a machine, as its own page: the Mesh "Add node" wizard in the console's frame. A machine
 * joins by registering itself with a pre-auth key, so a caller who may not hand out keys is told so
 * rather than shown a form the server would refuse.
 */
function AddMachinePage(): ReactElement {
  const { me } = Route.useRouteContext();

  useBreadcrumb("Add machine");

  return (
    <>
      <PageHeader
        title="Add machine"
        docs="ref/registration"
        description="Give the machine a key, run one command on it, and it joins the tailnet."
      />
      {can(me, "auth_keys") ? (
        <AddMachineWizard me={me} />
      ) : (
        <SectionEmpty
          title="Adding machines needs the pre-auth keys permission"
          description="Sign in on the machine with your own account instead, or ask an administrator for a key."
        />
      )}
    </>
  );
}
