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
import {
  approversMailto,
  hasNamedRecipient,
  isEmailEndpoint,
} from "~/components/webhooks/model.ts";

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

  const mailers = data.webhooks.filter(
    (hook) => isEmailEndpoint(hook.url) && hook.subscriptions.includes(newRequestEvent),
  );
  const problem = mailProblem({
    mailers: mailers.map((hook) => hook.url),
    mailAvailable: data.mailAvailable,
    approvers: data.approvers,
  });

  if (problem === null) {
    return null;
  }

  return (
    <Frame>
      <FramePanel className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-5 py-3">
        <p className="text-kumo-subtle">{problem}</p>
        {allowed && mailers.length === 0 && data.mailAvailable && data.approvers > 0 ? (
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

/**
 * What stops a new request from reaching anybody by email, or null when nothing does. An endpoint
 * that exists is not the same as one that sends: a mail server has to be configured, and an
 * endpoint addressed only to the approvers reaches nobody while no approver has an address.
 */
function mailProblem({
  mailers,
  mailAvailable,
  approvers,
}: {
  readonly mailers: readonly string[];
  readonly mailAvailable: boolean;
  readonly approvers: number;
}): string | null {
  if (mailers.length === 0) {
    if (!mailAvailable) {
      return "Nobody is emailed about a new request. Set notifications.smtp in the config file to send mail.";
    }

    if (approvers === 0) {
      return "Nobody is emailed about a new request: no user who may decide one has an email address.";
    }

    return `Nobody is emailed about a new request. ${plural(approvers, "person")} may decide them.`;
  }

  if (!mailAvailable) {
    return "New requests are set to be emailed, but no mail server is configured. Set notifications.smtp in the config file.";
  }

  // Somebody named outright is mailed whatever the roles say.
  if (mailers.some((url) => hasNamedRecipient(url))) {
    return null;
  }

  return approvers === 0
    ? "New requests are emailed to the approvers, but no user who may decide one has an email address, so the mail goes nowhere."
    : null;
}
