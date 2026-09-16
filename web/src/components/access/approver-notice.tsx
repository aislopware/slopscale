import { Button } from "@cloudflare/kumo/components/button";
import { EnvelopeSimpleIcon } from "@phosphor-icons/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import { invalidate, webhooksQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { plural } from "~/components/overview/plural.ts";
import { Frame, FramePanel } from "~/components/ui/frame.tsx";
import { toast } from "~/components/ui/toast.ts";
import { approversMailto, notifiesApprovers } from "~/components/webhooks/model.ts";

const newRequestEvent = "accessRequestCreated";

/**
 * Whether new requests reach anybody, and the one click that fixes it. A request nobody is told
 * about waits until an approver happens to open this page, which is the whole problem, so the row
 * sits above the table rather than in a settings page nobody visits.
 *
 * It says nothing at all once an endpoint mails the approvers: a solved problem should not keep
 * talking.
 */
export function ApproverNotice({ me }: { readonly me: Me }): ReactElement | null {
  const queryClient = useQueryClient();
  const allowed = can(me, "webhooks");
  const webhooks = useQuery({ ...webhooksQuery, enabled: can(me, "webhooks:read") });
  const create = api.useMutation("post", "/api/v1/webhook", {
    onSuccess: async () => {
      toast.success("New requests will be emailed to the approvers");
      await invalidate(queryClient, "/api/v1/webhook");
    },
    onError: (error) => {
      toast.error("Could not set up the email", error);
    },
  });

  const { data } = webhooks;

  if (data === undefined) {
    return null;
  }

  const told = data.webhooks.some(
    (hook) => notifiesApprovers(hook.url) && hook.subscriptions.includes(newRequestEvent),
  );

  if (told) {
    return null;
  }

  return (
    <Frame>
      <FramePanel className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-5 py-3">
        <p className="text-kumo-subtle">{reason(data.mailAvailable, data.approvers)}</p>
        {allowed && data.mailAvailable && data.approvers > 0 ? (
          <Button
            size="sm"
            variant="secondary"
            icon={EnvelopeSimpleIcon}
            loading={create.isPending}
            onClick={() => {
              create.mutate({
                body: {
                  url: approversMailto,
                  providerType: "email",
                  description: "New access requests",
                  subscriptions: [newRequestEvent],
                },
              });
            }}
          >
            Email the approvers
          </Button>
        ) : null}
      </FramePanel>
    </Frame>
  );
}

/** Why nobody is emailed, in the words of whatever is missing. */
function reason(mailAvailable: boolean, approvers: number): string {
  if (!mailAvailable) {
    return "Nobody is emailed about a new request. Set notifications.smtp in the config file to send mail.";
  }

  if (approvers === 0) {
    return "Nobody is emailed about a new request: no user who may decide one has an email address.";
  }

  return `Nobody is emailed about a new request. ${plural(approvers, "person")} may decide them.`;
}
