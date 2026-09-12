import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ShareNetworkIcon, XIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Node, User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { useNodeMutations } from "~/components/machines/mutations.ts";
import { mayManageNode } from "~/components/machines/owner.ts";
import { ShareDialog } from "~/components/machines/share-dialog.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { isTagged, userLabel } from "~/lib/node.ts";

/** Who else may reach this machine; a share is only ever a hint to the policy. */
export function SharingSection({
  node,
  users,
  me,
}: {
  readonly node: Node;
  readonly users: readonly User[];
  readonly me: Me;
}): ReactElement {
  const mutations = useNodeMutations();
  const [sharing, setSharing] = useState(false);
  const canEdit = mayManageNode(me, node) && !isTagged(node);

  return (
    <>
      <Section
        title="Sharing"
        description="Users whose own machines may reach this one."
        actions={
          canEdit ? (
            <Button
              variant="secondary"
              icon={ShareNetworkIcon}
              onClick={() => {
                setSharing(true);
              }}
            >
              Share…
            </Button>
          ) : undefined
        }
      >
        {node.sharedWith.length === 0 ? (
          <SectionRow className="text-kumo-subtle">
            {isTagged(node)
              ? "Tagged machines are not shared. The policy grants access to them instead."
              : "Not shared with anyone."}
          </SectionRow>
        ) : (
          node.sharedWith.map((userId) => (
            <ShareRow
              key={userId}
              userId={userId}
              users={users}
              canEdit={canEdit}
              pending={mutations.unshare.isPending}
              onRemove={() => {
                mutations.unshare.mutate({ params: { path: { nodeId: node.id, userId } } });
              }}
            />
          ))
        )}
      </Section>
      <ShareDialog
        node={node}
        users={users}
        mutations={mutations}
        open={sharing}
        onOpenChange={setSharing}
      />
    </>
  );
}

function ShareRow({
  userId,
  users,
  canEdit,
  pending,
  onRemove,
}: {
  readonly userId: string;
  readonly users: readonly User[];
  readonly canEdit: boolean;
  readonly pending: boolean;
  readonly onRemove: () => void;
}): ReactElement {
  const user = users.find((candidate) => candidate.id === userId);
  const label = user === undefined ? `User ${userId}` : userLabel(user);

  return (
    <SectionRow className="flex items-center justify-between gap-4 py-2.5">
      <span className="flex min-w-0 items-center gap-2">
        <Avatar name={label} id={userId} src={user?.profilePicUrl} size="sm" />
        <span className="truncate">{label}</span>
      </span>
      {canEdit ? (
        <RowMenu label={`Actions for share with ${label}`} disabled={pending}>
          <DropdownMenu.Item icon={XIcon} variant="danger" onClick={onRemove}>
            Stop sharing
          </DropdownMenu.Item>
        </RowMenu>
      ) : null}
    </SectionRow>
  );
}
