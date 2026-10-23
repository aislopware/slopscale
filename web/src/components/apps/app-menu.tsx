import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { App } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteAppDialog } from "~/components/apps/app-dialogs.tsx";
import { useAppMutations } from "~/components/apps/mutations.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/**
 * Edit and delete for one app, gated by the policy scope that writes the definition. Editing opens
 * the app's page.
 */
export function AppMenu({ app, me }: { readonly app: App; readonly me: Me }): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useAppMutations();
  const writable = can(me, "policy_file");

  return (
    <>
      <RowMenu label={`Actions for app ${app.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({ to: "/apps/$appId", params: { appId: app.id } });
          }}
        >
          {writable ? "Edit" : "View"}
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          disabled={!writable}
          onClick={() => {
            setDeleting(true);
          }}
        >
          Delete…
        </DropdownMenu.Item>
      </RowMenu>
      <DeleteAppDialog app={app} open={deleting} onOpenChange={setDeleting} mutations={mutations} />
    </>
  );
}
