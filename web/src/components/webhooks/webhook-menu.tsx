import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  ArrowsClockwiseIcon,
  ClockCounterClockwiseIcon,
  PaperPlaneTiltIcon,
  PencilSimpleIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Webhook } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";
import { DeliveriesDialog } from "~/components/webhooks/deliveries-dialog.tsx";
import { useWebhookMutations } from "~/components/webhooks/mutations.ts";
import {
  DeleteWebhookDialog,
  RotateSecretDialog,
  WebhookDialog,
} from "~/components/webhooks/webhook-dialogs.tsx";

type Dialog = "deliveries" | "edit" | "rotate" | "delete";

/** Test, edit, rotate and delete for one webhook, gated by the settings scope. */
export function WebhookMenu({
  webhook,
  eventTypes,
  me,
}: {
  readonly webhook: Webhook;
  readonly eventTypes: readonly string[];
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useWebhookMutations();
  const writable = can(me, "webhooks");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };
  const sendTest = (): void => {
    mutations.test.mutate(
      { params: { path: { id: webhook.id } } },
      {
        onSuccess: (data) => {
          if (data.delivered) {
            toast.success(`Test event delivered (HTTP ${data.status})`);
          } else {
            toast.error(`Test event failed (${data.status})`);
          }
        },
        onError: (error) => {
          toast.error(errorMessage(error));
        },
      },
    );
  };

  return (
    <>
      <RowMenu label={`Actions for webhook ${webhook.url}`}>
        <DropdownMenu.Item
          icon={PaperPlaneTiltIcon}
          disabled={!writable || mutations.test.isPending}
          onClick={sendTest}
        >
          Send test event
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ClockCounterClockwiseIcon}
          onClick={() => {
            setDialog("deliveries");
          }}
        >
          Deliveries…
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          disabled={!writable}
          onClick={() => {
            setDialog("edit");
          }}
        >
          Edit…
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ArrowsClockwiseIcon}
          disabled={!writable}
          onClick={() => {
            setDialog("rotate");
          }}
        >
          Rotate secret…
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
      </RowMenu>
      <DeliveriesDialog webhook={webhook} open={dialog === "deliveries"} onOpenChange={close} />
      <WebhookDialog
        webhook={webhook}
        eventTypes={eventTypes}
        open={dialog === "edit"}
        onOpenChange={close}
        mutations={mutations}
      />
      <RotateSecretDialog
        webhook={webhook}
        open={dialog === "rotate"}
        onOpenChange={close}
        mutations={mutations}
      />
      <DeleteWebhookDialog
        webhook={webhook}
        open={dialog === "delete"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
