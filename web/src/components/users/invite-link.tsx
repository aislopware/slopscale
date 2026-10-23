import { Button } from "@cloudflare/kumo/components/button";
import type { MethodResponse } from "openapi-react-query";
import type { ReactElement, ReactNode } from "react";

import type { api } from "~/api/client.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DialogFooter } from "~/components/ui/dialog.tsx";
import { UrlText } from "~/components/ui/url-text.tsx";
import { sentence } from "~/components/users/invites.ts";

/**
 * What the server hands back once, when an invitation is created or re-sent. Both calls answer with
 * the same body, so one view renders either.
 */
export type InviteResult = MethodResponse<typeof api, "post", "/api/v1/invite">;

/** The link an invitation carries, ready to copy, with what became of the email. Shown once. */
export function InviteLink({ result }: { readonly result: InviteResult }): ReactElement {
  return (
    <>
      <div className="rounded-lg bg-kumo-recessed p-3 ring ring-kumo-line">
        {/* A button centres its text by default, and a wrapped link reads as a column of fragments
            unless it starts at the left edge with the icon on its first line. */}
        <CopyText
          value={result.url}
          display={<UrlText url={result.url} />}
          wrap
          label="Copy invitation link"
          className="max-w-none items-start text-left"
        />
      </div>
      <MailOutcome result={result} />
    </>
  );
}

/** A re-sent invitation's new link in its dialog, with the Done that closes it. */
export function InviteResultView({
  result,
  onDone,
}: {
  readonly result: InviteResult;
  readonly onDone: () => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-4">
      <Callout
        title="Copy the link now"
        description="It is shown only here. Re-send the invitation to get a fresh link."
      />
      <InviteLink result={result} />
      <DialogFooter>
        <Button variant="primary" onClick={onDone}>
          Done
        </Button>
      </DialogFooter>
    </div>
  );
}

/** Whether the server mailed the link, and why it did not when it could not. */
function MailOutcome({ result }: { readonly result: InviteResult }): ReactNode {
  if (result.emailSent) {
    return <p className="text-kumo-subtle">{`Email sent to ${result.invite.email}.`}</p>;
  }

  if (result.emailError !== undefined && result.emailError !== "") {
    return (
      <Callout
        tone="warning"
        title="The email was not sent"
        description={`${sentence(result.emailError)} Send the link yourself.`}
      />
    );
  }

  return <p className="text-kumo-subtle">This server sends no mail. Send the link yourself.</p>;
}
