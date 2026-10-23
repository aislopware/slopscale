import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  ArrowsClockwiseIcon,
  ClockCounterClockwiseIcon,
  PaperPlaneTiltIcon,
  PencilSimpleIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Webhook } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { RowMenu } from "~/components/ui/row-menu.tsx";
import { toast } from "~/components/ui/toast.ts";
import { DeliveriesSheet } from "~/components/webhooks/deliveries-sheet.tsx";
import { useWebhookMutations } from "~/components/webhooks/mutations.ts";
import type { WebhookMutations } from "~/components/webhooks/mutations.ts";
import { DeleteWebhookDialog, RotateSecretDialog } from "~/components/webhooks/webhook-dialogs.tsx";

type Dialog = "deliveries" | "rotate" | "delete";

/** Sends the webhook a test event and says in a toast how the endpoint answered. */
function sendTest(mutations: WebhookMutations, webhook: Webhook): void {
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
}

/**
 * Test, deliveries, edit, rotate and delete for one webhook, gated by the webhooks scope. Editing
 * opens the webhook's page; the deliveries open in a side sheet.
 */
export function WebhookMenu({
  webhook,
  me,
}: {
  readonly webhook: Webhook;
  readonly me: Me;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const navigate = useNavigate();
  const mutations = useWebhookMutations();
  const writable = can(me, "webhooks");
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <RowMenu label={`Actions for webhook ${webhook.url}`}>
        <DropdownMenu.Item
          icon={PaperPlaneTiltIcon}
          disabled={!writable || mutations.test.isPending}
          onClick={() => {
            sendTest(mutations, webhook);
          }}
        >
          Send test event
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ClockCounterClockwiseIcon}
          onClick={() => {
            setDialog("deliveries");
          }}
        >
          Deliveries
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={PencilSimpleIcon}
          onClick={() => {
            void navigate({
              to: "/integrations/webhooks/$webhookId",
              params: { webhookId: webhook.id },
            });
          }}
        >
          {writable ? "Edit" : "View"}
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
      <DeliveriesSheet webhook={webhook} open={dialog === "deliveries"} onOpenChange={close} />
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

/**
 * The webhook page's own actions beside its title: send a test event, read the deliveries in the
 * side sheet, and rotate the signing secret behind its confirmation.
 */
export function WebhookPageActions({
  webhook,
  canEdit,
}: {
  readonly webhook: Webhook;
  readonly canEdit: boolean;
}): ReactElement {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const mutations = useWebhookMutations();
  const close = (open: boolean): void => {
    if (!open) {
      setDialog(null);
    }
  };

  return (
    <>
      <Button
        variant="secondary"
        icon={PaperPlaneTiltIcon}
        disabled={!canEdit}
        loading={mutations.test.isPending}
        onClick={() => {
          sendTest(mutations, webhook);
        }}
      >
        Send test event
      </Button>
      <Button
        variant="secondary"
        icon={ArrowsClockwiseIcon}
        disabled={!canEdit}
        onClick={() => {
          setDialog("rotate");
        }}
      >
        Rotate secret
      </Button>
      <Button
        variant="secondary"
        icon={ClockCounterClockwiseIcon}
        onClick={() => {
          setDialog("deliveries");
        }}
      >
        Deliveries
      </Button>
      <DeliveriesSheet webhook={webhook} open={dialog === "deliveries"} onOpenChange={close} />
      <RotateSecretDialog
        webhook={webhook}
        open={dialog === "rotate"}
        onOpenChange={close}
        mutations={mutations}
      />
    </>
  );
}
