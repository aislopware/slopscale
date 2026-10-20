import { LinkButton } from "@cloudflare/kumo/components/button";
import { createFileRoute } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import { can, canListUsers } from "~/auth/me.ts";
import { FocusedFlow } from "~/components/layout/focused-flow.tsx";
import { AddMachineWizard } from "~/components/machines/add-wizard.tsx";
import { cardClass } from "~/components/ui/card.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

export const Route = createFileRoute("/_focus/machines/add")({
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(nodesQuery),
      canListUsers(context.me) ? context.queryClient.query(usersQuery) : Promise.resolve(),
    ]);
  },
  component: AddMachinePage,
});

/**
 * Adding a machine, as Cloudflare's Mesh adds a node: a full-screen flow outside the dashboard, so
 * the sidebar does not compete with the one thing being done. A machine joins by registering itself
 * with a pre-auth key, so a caller who may not hand out keys is told so rather than shown a form
 * the server would refuse.
 */
function AddMachinePage(): ReactElement {
  const { me } = Route.useRouteContext();

  if (can(me, "auth_keys")) {
    return <AddMachineWizard me={me} />;
  }

  return (
    <FocusedFlow title="Add machine" section="Machines" closeHref="/machines">
      <div className={cardClass}>
        <SectionEmpty
          title="Adding machines needs the pre-auth keys permission"
          description="Sign in on the machine with your own account instead, or ask an administrator for a key."
          contents={
            <LinkButton href="/machines" variant="secondary">
              Back to machines
            </LinkButton>
          }
        />
      </div>
    </FocusedFlow>
  );
}
