import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Group, Network, Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { DeleteNetworkDialog, NetworkDialog } from "~/components/networks/network-dialogs.tsx";

const actionsIconSize = 18;

type Dialog = "edit" | "delete";

/** Edit and delete for one network, gated by the routes scope. */
export function NetworkMenu({
  network,
  groups,
  nodes,
  me,
}: {
  readonly network: Network;
  readonly groups: readonly Group[];
  readonly nodes: readonly Node[];
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useNetworkMutations();
  const writable = can(me, "devices:routes");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              variant="ghost"
              shape="square"
              size="sm"
              icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
              aria-label={`Actions for network ${network.name}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
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
        </DropdownMenu.Content>
      </DropdownMenu>
      <NetworkDialog
        network={network}
        groups={groups}
        nodes={nodes}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteNetworkDialog
        network={network}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
