import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { CheckIcon, DotsThreeIcon, ProhibitIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { useAccessMutations } from "~/components/access/mutations.ts";
import {
  ApproveRequestDialog,
  CancelRequestDialog,
  DenyRequestDialog,
} from "~/components/access/request-dialogs.tsx";
import type { RequestRow } from "~/components/access/request-model.ts";

const actionsIconSize = 18;

type Dialog = "approve" | "deny" | "cancel";

/**
 * Approve, deny and withdraw for one request. An approver gets all three on a pending request and
 * delete on a decided one; the requester only withdraws their own pending request.
 */
export function RequestMenu({
  request,
  canDecide,
  own,
}: {
  readonly request: RequestRow;
  readonly canDecide: boolean;
  readonly own: boolean;
}): ReactElement | null {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useAccessMutations();
  const pending = request.status === "pending";
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  if (!canDecide && !(own && pending)) {
    return null;
  }

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
              aria-label={`Actions for request ${request.id}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
          {canDecide && pending ? (
            <>
              <DropdownMenu.Item
                icon={CheckIcon}
                onClick={() => {
                  setDialog("approve");
                }}
              >
                Approve…
              </DropdownMenu.Item>
              <DropdownMenu.Item
                icon={ProhibitIcon}
                onClick={() => {
                  setDialog("deny");
                }}
              >
                Deny…
              </DropdownMenu.Item>
              <DropdownMenu.Separator />
            </>
          ) : null}
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            onClick={() => {
              setDialog("cancel");
            }}
          >
            {pending ? "Withdraw…" : "Delete…"}
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
      <ApproveRequestDialog
        request={request}
        open={dialog === "approve"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DenyRequestDialog
        request={request}
        open={dialog === "deny"}
        onOpenChange={close}
        mutations={mutations}
      />
      <CancelRequestDialog
        request={request}
        open={dialog === "cancel"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
