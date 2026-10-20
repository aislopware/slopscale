import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Group } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { DeleteGroupDialog } from "~/components/access/group-dialogs.tsx";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Edit and delete for one group; the caller hides it for the builtin group. */
export function GroupMenu({
  group,
  rules,
  me,
}: {
  readonly group: Group;
  readonly rules: readonly AccessRule[];
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const writable = can(me, "policy_file");

  return (
    <>
      <RowMenu label={`Actions for group ${group.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({ to: "/policy/groups/$groupId", params: { groupId: group.id } });
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
      <DeleteGroupDialog
        group={group}
        rules={rules}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}
