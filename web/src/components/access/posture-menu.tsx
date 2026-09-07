import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon, PencilSimpleIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { AccessRule, Posture } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { useAccessMutations } from "~/components/access/mutations.ts";
import { DeletePostureDialog, PostureDialog } from "~/components/access/posture-dialogs.tsx";

const actionsIconSize = 18;

type Dialog = "edit" | "delete";

/** Edit and delete for one posture, gated by the policy scope. */
export function PostureMenu({
  posture,
  rules,
  geoIpAvailable,
  me,
}: {
  readonly posture: Posture;
  readonly rules: readonly AccessRule[];
  readonly geoIpAvailable: boolean;
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
              aria-label={`Actions for posture ${posture.name}`}
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
      <PostureDialog
        posture={posture}
        geoIpAvailable={geoIpAvailable}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeletePostureDialog
        posture={posture}
        rules={rules}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
