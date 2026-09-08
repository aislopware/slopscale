import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { DotsThreeIcon, PaperPlaneTiltIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Invite } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { toast } from "~/components/ui/toast.ts";
import { InviteResultView } from "~/components/users/invite-dialog.tsx";
import type { InviteResult } from "~/components/users/invite-dialog.tsx";
import { useInviteMutations } from "~/components/users/mutations.ts";

const actionsIconSize = 18;

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
  const reason = writable ? undefined : "Your credentials may not change users";

  function resend(): void {
    mutations.resend.mutate(
      { params: { path: { id: invite.id } }, body: {} },
      {
        onSuccess: setResult,
        onError: (error) => {
          toast.error("Could not re-send the invite", error);
        },
      },
    );
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
              aria-label={`Actions for the invite to ${invite.email}`}
            />
          }
        />
        <DropdownMenu.Content align="end">
          <DisabledReason reason={reason}>
            <DropdownMenu.Item icon={PaperPlaneTiltIcon} disabled={!writable} onClick={resend}>
              Re-send
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
        </DropdownMenu.Content>
      </DropdownMenu>
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
          description="The link from the earlier message has stopped working."
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
        description={`The link sent to ${invite.email} stops working. Invite the address again to send a new one.`}
        confirmLabel="Revoke invite"
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
