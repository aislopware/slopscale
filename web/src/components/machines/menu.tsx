import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  ArrowsClockwiseIcon,
  CaretDownIcon,
  CheckIcon,
  ClockIcon,
  FingerprintIcon,
  GlobeIcon,
  PathIcon,
  PauseIcon,
  PencilSimpleIcon,
  PlayIcon,
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
  ClientUpdateDialog,
  DeleteDialog,
  ExpireDialog,
  RenameDialog,
  ResetAttestationDialog,
  SuspendDialog,
  TagsDialog,
} from "~/components/machines/dialogs.tsx";
import { reportClientUpdate, useNodeMutations } from "~/components/machines/mutations.ts";
import { ownerId } from "~/components/machines/owner.ts";
import { RoutesDialog } from "~/components/machines/routes-dialog.tsx";
import { ShareDialog } from "~/components/machines/share-dialog.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";
import { advertisesExit, isTagged, nodeName } from "~/lib/node.ts";

type Dialog =
  | "rename"
  | "tags"
  | "routes"
  | "share"
  | "attestation"
  | "update"
  | "suspend"
  | "expire"
  | "delete";

export interface MachineMenuProps {
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  /** Renders the trigger as a labelled button instead of the icon. */
  readonly labelled?: boolean;
  /** Leaves out expire and remove, for pages that offer them in a danger zone of their own. */
  readonly hideDestructive?: boolean;
}

/** Every action on one machine, gated by the caller's scopes. */
export function MachineMenu({
  node,
  me,
  users,
  labelled = false,
  hideDestructive = false,
}: MachineMenuProps): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useNodeMutations();
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  const items = (
    <MachineMenuItems
      node={node}
      me={me}
      mutations={mutations}
      hideDestructive={hideDestructive}
      onOpen={setDialog}
    />
  );

  return (
    <>
      {labelled ? (
        <DropdownMenu>
          <DropdownMenu.Trigger
            render={
              <Button variant="secondary">
                Actions
                <CaretDownIcon />
              </Button>
            }
          />
          <DropdownMenu.Content align="end">{items}</DropdownMenu.Content>
        </DropdownMenu>
      ) : (
        <RowMenu label={`Actions for ${nodeName(node)}`}>{items}</RowMenu>
      )}
      <MachineDialogs
        dialog={dialog}
        node={node}
        me={me}
        users={users}
        mutations={mutations}
        onOpenChange={close}
      />
    </>
  );
}

/** Suspend asks first; lifting a suspension is one click, since it only gives access back. */
function SuspendItem({
  node,
  core,
  mutations,
  onOpen,
}: {
  readonly node: Node;
  readonly core: boolean;
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly onOpen: (dialog: Dialog) => void;
}): ReactElement {
  if (node.suspended) {
    return (
      <DropdownMenu.Item
        icon={PlayIcon}
        disabled={!core}
        onClick={() => {
          mutations.suspend.mutate(
            { params: { path: { nodeId: node.id } }, body: { suspended: false } },
            {
              onSuccess: () => {
                toast.success("Suspension lifted");
              },
              onError: (error) => {
                toast.error("Could not lift the suspension", error);
              },
            },
          );
        }}
      >
        Lift suspension
      </DropdownMenu.Item>
    );
  }

  return (
    <DropdownMenu.Item
      icon={PauseIcon}
      disabled={!core}
      onClick={() => {
        onOpen("suspend");
      }}
    >
      Suspend…
    </DropdownMenu.Item>
  );
}

/**
 * Asks the machine to update itself. Only while it is connected and behind: the request is a live
 * round trip to the client, and the client is the one that decides, so the answer is reported. The
 * update restarts Tailscale on the machine, so the dialog asks before anything is sent.
 */
function UpdateClientItem({ onOpen }: { readonly onOpen: (dialog: Dialog) => void }): ReactElement {
  return (
    <DropdownMenu.Item
      icon={ArrowsClockwiseIcon}
      onClick={() => {
        onOpen("update");
      }}
    >
      Update client…
    </DropdownMenu.Item>
  );
}

/** What the machine offers to route, and whether every client should prefer it as the way out. */
function RouteItems({
  node,
  routes,
  mutations,
  onOpen,
}: {
  readonly node: Node;
  readonly routes: boolean;
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly onOpen: (dialog: Dialog) => void;
}): ReactElement {
  return (
    <>
      {node.availableRoutes.length > 0 || node.approvedRoutes.length > 0 ? (
        <DropdownMenu.Item
          icon={PathIcon}
          disabled={!routes}
          onClick={() => {
            onOpen("routes");
          }}
        >
          Approve routes…
        </DropdownMenu.Item>
      ) : null}
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
          {node.globalExitNode ? "Clear global exit node" : "Use as global exit node"}
        </DropdownMenu.Item>
      ) : null}
    </>
  );
}

function MachineMenuItems({
  node,
  me,
  mutations,
  hideDestructive,
  onOpen,
}: {
  readonly node: Node;
  readonly me: Me;
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly hideDestructive: boolean;
  readonly onOpen: (dialog: Dialog) => void;
}): ReactElement {
  const core = can(me, "devices:core");
  const routes = can(me, "devices:routes");
  const ownNode = me.user !== undefined && ownerId(node) === me.user.id;
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
      <RouteItems node={node} routes={routes} mutations={mutations} onOpen={onOpen} />
      <DropdownMenu.Item
        icon={ShareNetworkIcon}
        disabled={!share || isTagged(node)}
        onClick={() => {
          onOpen("share");
        }}
      >
        Share…
      </DropdownMenu.Item>
      {core && node.updateAvailable && node.online ? <UpdateClientItem onOpen={onOpen} /> : null}
      {core && node.hardwareAttestation !== undefined ? (
        <DropdownMenu.Item
          icon={FingerprintIcon}
          onClick={() => {
            onOpen("attestation");
          }}
        >
          Reset hardware attestation…
        </DropdownMenu.Item>
      ) : null}
      {hideDestructive ? null : (
        <>
          <DropdownMenu.Separator />
          <SuspendItem node={node} core={core} mutations={mutations} onOpen={onOpen} />
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
      )}
    </>
  );
}

/**
 * Every dialog stays mounted and is driven by `open` so its transition plays; each one keeps its
 * form state in a child of the popup, which Base UI unmounts on close.
 */
function MachineDialogs({
  dialog,
  node,
  me,
  users,
  mutations,
  onOpenChange,
}: {
  readonly dialog: Dialog | null;
  readonly node: Node;
  readonly me: Me;
  readonly users: readonly User[];
  readonly mutations: ReturnType<typeof useNodeMutations>;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const props = { node, mutations, onOpenChange };

  return (
    <>
      <RenameDialog open={dialog === "rename"} {...props} />
      <TagsDialog open={dialog === "tags"} me={me} {...props} />
      <RoutesDialog open={dialog === "routes"} {...props} />
      <ShareDialog open={dialog === "share"} users={users} {...props} />
      <ResetAttestationDialog open={dialog === "attestation"} {...props} />
      <ClientUpdateDialog
        name={nodeName(node)}
        open={dialog === "update"}
        onOpenChange={onOpenChange}
        pending={mutations.updateClient.isPending}
        onConfirm={() => {
          mutations.updateClient.mutate(
            { params: { path: { nodeId: node.id } }, body: {} },
            {
              onSuccess: (update) => {
                onOpenChange(false);
                reportClientUpdate(update);
              },
            },
          );
        }}
      />
      <SuspendDialog open={dialog === "suspend"} {...props} />
      <ExpireDialog open={dialog === "expire"} {...props} />
      <DeleteDialog open={dialog === "delete"} {...props} />
    </>
  );
}
