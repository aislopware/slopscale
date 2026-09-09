import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { App } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { AppDialog, DeleteAppDialog } from "~/components/apps/app-dialogs.tsx";
import { useAppMutations } from "~/components/apps/mutations.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";

type Dialog = "edit" | "delete";

/** Edit and delete for one app, gated by the policy scope that writes the definition. */
export function AppMenu({ app, me }: { readonly app: App; readonly me: Me }): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useAppMutations();
  const writable = can(me, "policy_file");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <RowMenu label={`Actions for app ${app.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          disabled={!writable}
          onClick={() => {
            setDialog("edit");
          }}
        >
          Edit…
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={TrashIcon}
          variant="danger"
          disabled={!writable}
          onClick={() => {
            setDialog("delete");
          }}
        >
          Delete…
        </DropdownMenu.Item>
      </RowMenu>
      <AppDialog app={app} open={dialog === "edit"} onOpenChange={close} mutations={mutations} />
      <DeleteAppDialog
        app={app}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
