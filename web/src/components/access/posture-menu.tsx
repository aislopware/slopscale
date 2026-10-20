import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DeletePostureDialog } from "~/components/access/posture-dialogs.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";

/** Edit and delete for one posture, gated by the policy scope. Editing opens its own page. */
export function PostureMenu({
  posture,
  rules,
  me,
}: {
  readonly posture: Posture;
  readonly rules: readonly AccessRule[];
  readonly me: Me;
}): ReactElement {
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const mutations = useAccessMutations();
  const writable = can(me, "policy_file");

  return (
    <>
      <RowMenu label={`Actions for posture ${posture.name}`}>
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({
              to: "/policy/postures/$postureId",
              params: { postureId: posture.id },
            });
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
      <DeletePostureDialog
        posture={posture}
        rules={rules}
        open={deleting}
        onOpenChange={setDeleting}
        mutations={mutations}
      />
    </>
  );
}
