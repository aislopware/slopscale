import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  CaretDownIcon,
  CheckIcon,
  ClockIcon,
  DotsThreeIcon,
  GlobeIcon,
  PathIcon,
  PencilSimpleIcon,
  ShareNetworkIcon,
  TagIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import {
  DeleteDialog,
  ExpireDialog,
  RenameDialog,
  TagsDialog,
} from "~/components/machines/dialogs.tsx";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { RoutesDialog } from "~/components/machines/routes-dialog.tsx";
import { ShareDialog } from "~/components/machines/share-dialog.tsx";
import { advertisesExit, isTagged } from "~/lib/node.ts";

const actionsIconSize = 18;

type Dialog = "rename" | "tags" | "routes" | "share" | "expire" | "delete";

export interface MachineMenuProps {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  /** Renders the trigger as a labelled button instead of the icon. */
  readonly labelled?: boolean;
}

/** Every action on one machine, gated by the caller's scopes. */
export function MachineMenu({ node, me, users, labelled = false }: MachineMenuProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useNodeMutations();
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
            labelled ? (
              <Button variant="secondary">
                Actions
                <CaretDownIcon />
              </Button>
            ) : (
              <Button
                variant="ghost"
                shape="square"
                size="sm"
                icon={<DotsThreeIcon size={actionsIconSize} weight="bold" />}
                aria-label="Actions"
              />
            )
          }
        />
        <DropdownMenu.Content align="end">
          <MachineMenuItems node={node} me={me} mutations={mutations} onOpen={setDialog} />
        </DropdownMenu.Content>
      </DropdownMenu>
      <MachineDialogs
        dialog={dialog}
        node={node}
        users={users}
        mutations={mutations}
        onOpenChange={close}
      />
    </>
  );
}

function MachineMenuItems({
  node,
  me,
  mutations,
  onOpen,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly onOpen: (dialog: Dialog) => void;
}): ReactElement {
  const core = can(me, "devices:core");
  const routes = can(me, "devices:routes");
  const ownNode = me.user !== undefined && !isTagged(node) && node.user.id === me.user.id;
  const share = core || ownNode;

  return (
    <>
      {core && !node.approved ? (
        <DropdownMenu.Item
          icon={CheckIcon}
          onClick={() => {
            mutations.approve.mutate({ params: { path: { nodeId: node.id } }, body: {} });
          }}
        >
          Approve
        </DropdownMenu.Item>
      ) : null}
      <DropdownMenu.Item
        icon={PencilSimpleIcon}
        disabled={!core}
        onClick={() => {
          onOpen("rename");
        }}
      >
        Rename…
      </DropdownMenu.Item>
      <DropdownMenu.Item
        icon={TagIcon}
        disabled={!core}
        onClick={() => {
          onOpen("tags");
        }}
      >
        Edit tags…
      </DropdownMenu.Item>
      <DropdownMenu.Item
        icon={PathIcon}
        disabled={!routes}
        onClick={() => {
          onOpen("routes");
        }}
      >
        Approve routes…
      </DropdownMenu.Item>
      {advertisesExit(node) ? (
        <DropdownMenu.Item
          icon={GlobeIcon}
          disabled={!routes}
          onClick={() => {
            mutations.setGlobalExitNode.mutate({
              params: { path: { nodeId: node.id } },
              body: { enabled: !node.globalExitNode },
            });
          }}
        >
          {node.globalExitNode ? "Stop being global exit node" : "Use as global exit node"}
        </DropdownMenu.Item>
      ) : null}
      <DropdownMenu.Item
        icon={ShareNetworkIcon}
        disabled={!share || isTagged(node)}
        onClick={() => {
          onOpen("share");
        }}
      >
        Share…
      </DropdownMenu.Item>
      <DropdownMenu.Separator />
      <DropdownMenu.Item
        icon={ClockIcon}
        disabled={!core}
        onClick={() => {
          onOpen("expire");
        }}
      >
        Expire key…
      </DropdownMenu.Item>
      <DropdownMenu.Item
        icon={TrashIcon}
        variant="danger"
        disabled={!core}
        onClick={() => {
          onOpen("delete");
        }}
      >
        Remove…
      </DropdownMenu.Item>
    </>
  );
}

/**
 * All six dialogs stay mounted and are driven by `open` so their transitions play; each one keeps
 * its form state in a child of the popup, which Base UI unmounts on close.
 */
function MachineDialogs({
  dialog,
  node,
  users,
  mutations,
  onOpenChange,
}: {
  readonly dialog: Dialog | null;
  readonly node: Node;
  readonly users: readonly User[];
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const props = { node, mutations, onOpenChange };

  return (
    <>
      <RenameDialog open={dialog === "rename"} {...props} />
      <TagsDialog open={dialog === "tags"} {...props} />
      <RoutesDialog open={dialog === "routes"} {...props} />
      <ShareDialog open={dialog === "share"} users={users} {...props} />
      <ExpireDialog open={dialog === "expire"} {...props} />
      <DeleteDialog open={dialog === "delete"} {...props} />
    </>
  );
}
