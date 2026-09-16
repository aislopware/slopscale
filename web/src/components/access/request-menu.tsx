import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  CheckIcon,
  ClockCounterClockwiseIcon,
  ProhibitIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { useAccessMutations } from "~/components/access/mutations.ts";
import {
  ApproveRequestDialog,
  CancelRequestDialog,
  DenyRequestDialog,
  RevokeRequestDialog,
} from "~/components/access/request-dialogs.tsx";
import type { RequestRow } from "~/components/access/request-model.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";

type Dialog = "approve" | "deny" | "cancel" | "revoke";

/**
 * Approve, deny, revoke and withdraw for one request. An approver decides a pending request, ends
 * the access of one that is in effect, and deletes a request that granted nothing; the requester
 * only withdraws their own pending request.
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
  const active = request.phase === "active";
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
      <RowMenu label={`Actions for request ${request.id}`}>
        {canDecide && active ? (
          <>
            <DropdownMenu.Item
              icon={ClockCounterClockwiseIcon}
              variant="danger"
              onClick={() => {
                setDialog("revoke");
              }}
            >
              Revoke access…
            </DropdownMenu.Item>
            <DropdownMenu.Separator />
          </>
        ) : null}
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
        {/* Deleting the record of access that is in effect would leave the membership behind
            with nothing to show it, so the server refuses it; revoke comes first. */}
        {active ? null : (
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            onClick={() => {
              setDialog("cancel");
            }}
          >
            {pending ? "Withdraw…" : "Delete…"}
          </DropdownMenu.Item>
        )}
      </RowMenu>
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
      <RevokeRequestDialog
        request={request}
        open={dialog === "revoke"}
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
