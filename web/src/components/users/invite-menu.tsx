import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { PaperPlaneTiltIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Invite } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";
import { InviteResultView } from "~/components/users/invite-dialog.tsx";
import type { InviteResult } from "~/components/users/invite-dialog.tsx";
import { cannotChangeUsers } from "~/components/users/menu.tsx";
import { useInviteMutations } from "~/components/users/mutations.ts";

/** Re-send or revoke one invitation. A re-send mints a new link, so it is shown like a new one. */
export function InviteMenu({
  invite,
  me,
}: {
  readonly invite: Invite;
  readonly me: Me;
}): ReactElement {
  const mutations = useInviteMutations();
  const [result, setResult] = useState<InviteResult | null>(null);
  const [confirming, setConfirming] = useState(false);
  const writable = can(me, "users");
  const reason = writable ? undefined : cannotChangeUsers;

  function resend(): void {
    mutations.resend.mutate(
      { params: { path: { id: invite.id } }, body: {} },
      {
        onSuccess: setResult,
        onError: (error) => {
          toast.error("Could not resend the invitation", error);
        },
      },
    );
  }

  return (
    <>
      <RowMenu label={`Actions for the invite to ${invite.email}`}>
        <DisabledReason reason={reason}>
          <DropdownMenu.Item icon={PaperPlaneTiltIcon} disabled={!writable} onClick={resend}>
            Resend
          </DropdownMenu.Item>
        </DisabledReason>
        <DropdownMenu.Separator />
        <DisabledReason reason={reason}>
          <DropdownMenu.Item
            icon={TrashIcon}
            variant="danger"
            disabled={!writable}
            onClick={() => {
              setConfirming(true);
            }}
          >
            Revoke…
          </DropdownMenu.Item>
        </DisabledReason>
      </RowMenu>
      <DialogRoot
        open={result !== null}
        onOpenChange={(open) => {
          if (!open) {
            setResult(null);
          }
        }}
      >
        <DialogContent
          size="base"
          title="New invitation link"
          description="The previous link has stopped working."
        >
          {result === null ? null : (
            <InviteResultView
              result={result}
              onDone={() => {
                setResult(null);
              }}
            />
          )}
        </DialogContent>
      </DialogRoot>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Revoke this invitation?"
        description={`The link sent to ${invite.email} stops working. Send a new invitation to reissue it.`}
        confirmLabel="Revoke invitation"
        loading={mutations.revoke.isPending}
        {...(mutations.revoke.isError ? { error: errorMessage(mutations.revoke.error) } : {})}
        onConfirm={() => {
          mutations.revoke.mutate(
            { params: { path: { id: invite.id } } },
            {
              onSuccess: () => {
                setConfirming(false);
              },
            },
          );
        }}
      />
    </>
  );
}
