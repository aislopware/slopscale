import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteGroupDialog, GroupDialog } from "~/components/access/group-dialogs.tsx";
import { useAccessMutations } from "~/components/access/mutations.ts";

const actionsIconSize = 18;

type Dialog = "edit" | "delete";

/** Edit and delete for one group; the caller hides it for the builtin group. */
export function GroupMenu({
  group,
  nodes,
  users,
  rules,
  me,
}: {
  readonly group: Group;
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly rules: readonly AccessRule[];
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useAccessMutations();
  const writable = can(me, "policy_file");
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
              aria-label={`Actions for group ${group.name}`}
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
      <GroupDialog
        group={group}
        nodes={nodes}
        users={users}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteGroupDialog
        group={group}
        rules={rules}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
