import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Network } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useNetworkMutations } from "~/components/networks/mutations.ts";
import { DeleteNetworkDialog } from "~/components/networks/network-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Edit and delete for one network, gated by the routes scope. Editing opens the network's page. */
export function NetworkMenu({
  network,
  me,
}: {
  readonly network: Network;
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useNetworkMutations();
  const writable = can(me, "devices:routes");

  return (
    <>
      <RowMenu label={`Actions for network ${network.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({ to: "/networks/$networkId", params: { networkId: network.id } });
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
      <DeleteNetworkDialog
        network={network}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}
