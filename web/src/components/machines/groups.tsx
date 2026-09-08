import { Button } from "@cloudflare/kumo/components/button";
import { UsersThreeIcon, XIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Group, Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { MembershipDialog } from "~/components/access/membership-dialog.tsx";
import { groupsOfNode, isBuiltin } from "~/components/access/model.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { ownerId } from "~/components/machines/owner.ts";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";
import { userLabel } from "~/lib/node.ts";

/** Which groups the machine resolves into, and where each membership comes from. */
export function GroupsSection({
  node,
  groups,
  users,
  me,
}: {
  readonly node: Node;
  readonly groups: readonly Group[];
  readonly users: readonly User[];
  readonly me: Me;
}): ReactElement {
  const mutations = useAccessMutations();
  const [editing, setEditing] = useState(false);
  const canEdit = can(me, "policy_file");
  const members = groupsOfNode(groups, node);
  const direct = groups.filter((group) => group.nodeIds.includes(node.id)).map((group) => group.id);
  const owner = ownerId(node);
  const ownerName = users.find((user) => user.id === owner);

  return (
    <>
      <Section
        title="Groups"
        description="Rules are written between groups. This machine is in these."
        actions={
          canEdit ? (
            <Button
              variant="secondary"
              size="sm"
              icon={UsersThreeIcon}
              onClick={() => {
                setEditing(true);
              }}
            >
              Edit groups…
            </Button>
          ) : undefined
        }
      >
        {members.map((group) => (
          <GroupRow
            key={group.id}
            group={group}
            origin={membershipOrigin(group, node, ownerName)}
            removable={canEdit && group.nodeIds.includes(node.id)}
            pending={mutations.removeNode.isPending}
            onRemove={() => {
              mutations.removeNode.mutate(
                { params: { path: { id: group.id, nodeId: node.id } } },
                {
                  onSuccess: () => {
                    toast.success(
                      ownerName !== undefined && group.userIds.includes(ownerName.id)
                        ? `Direct membership removed; still in ${group.name} through ${userLabel(ownerName)}`
                        : `Removed from ${group.name}`,
                    );
                  },
                  onError: (error) => {
                    toast.error(errorMessage(error));
                  },
                },
              );
            }}
          />
        ))}
      </Section>
      <MembershipDialog
        title="Edit groups"
        description="Groups this machine is added to on its own. Groups it is in through its owner are changed on the user, not here."
        member={{ nodeId: node.id }}
        groups={groups}
        current={direct}
        open={editing}
        onOpenChange={setEditing}
        mutations={mutations}
      />
    </>
  );
}

/** Where a membership comes from; a machine can be in through both paths at once. */
function membershipOrigin(group: Group, node: Node, owner: User | undefined): string {
  if (isBuiltin(group)) {
    return "Every machine";
  }

  const origins: string[] = [];

  if (group.nodeIds.includes(node.id)) {
    origins.push("Added directly");
  }

  if (owner !== undefined && group.userIds.includes(owner.id)) {
    origins.push(`Through ${userLabel(owner)}`);
  }

  return origins.length === 0 ? "Through its owner" : origins.join(" · ");
}

function GroupRow({
  group,
  origin,
  removable,
  pending,
  onRemove,
}: {
  readonly group: Group;
  readonly origin: string;
  readonly removable: boolean;
  readonly pending: boolean;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <SectionRow className="flex items-center justify-between gap-4 py-2.5">
      <span className="flex min-w-0 flex-col gap-0.5">
        <Link
          to="/policy/groups"
          search={{ q: group.name }}
          className="truncate font-medium text-kumo-default hover:text-kumo-link"
        >
          {group.name}
        </Link>
        <span className="truncate text-xs text-kumo-subtle">{origin}</span>
      </span>
      {removable ? (
        <Button
          variant="ghost"
          shape="square"
          size="sm"
          icon={XIcon}
          aria-label={`Remove direct membership of ${group.name}`}
          title="Remove direct membership"
          loading={pending}
          onClick={onRemove}
        />
      ) : null}
    </SectionRow>
  );
}
