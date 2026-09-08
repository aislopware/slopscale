import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { EnvelopeSimpleIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import type { MethodResponse } from "openapi-react-query";
import { useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import type { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { groupsQuery } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { Callout } from "~/components/ui/callout.tsx";
import { CopyText } from "~/components/ui/copy-text.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import {
  defaultInviteExpiry,
  invitableRoles,
  inviteExpiryOptions,
  toInviteExpiry,
} from "~/components/users/invites.ts";
import type { InviteExpiry } from "~/components/users/invites.ts";
import { useInviteMutations } from "~/components/users/mutations.ts";
import { roleName } from "~/components/users/roles.ts";
import type { UserRole } from "~/components/users/roles.ts";

type Mutations = ReturnType<typeof useInviteMutations>;

/**
 * What the server hands back once, when an invitation is created or re-sent. Both calls answer with
 * the same body, so one view renders either.
 */
export type InviteResult = MethodResponse<typeof api, "post", "/api/v1/invite">;

/**
 * The users table's secondary action. "Add user" stays the page's one primary, because a local
 * account is what an operator reaches for when there is no identity provider to invite anyone
 * into.
 */
export function InviteUserButton({ me }: { readonly me: Me }): ReactElement {
  const [open, setOpen] = useState(false);
  const mutations = useInviteMutations();

  return (
    <>
      <Button
        variant="secondary"
        icon={EnvelopeSimpleIcon}
        disabled={!can(me, "users")}
        onClick={() => {
          setOpen(true);
        }}
      >
        Invite
      </Button>
      <InviteDialog open={open} onOpenChange={setOpen} me={me} mutations={mutations} />
    </>
  );
}

/** The users page's "Invite" action: the form, then the link the invitation carries. */
export function InviteDialog({
  open,
  onOpenChange,
  me,
  mutations,
}: {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly me: Me;
  readonly mutations: Mutations;
}): ReactElement {
  const [result, setResult] = useResettingResult(open);

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={result === null ? "Invite a user" : "Invitation link"}
        {...(result === null
          ? {
              description:
                "The first sign-in that opens the link, or whose verified email matches the address, creates the account with the role and groups you pick here.",
            }
          : {})}
      >
        {result === null ? (
          <InviteForm me={me} mutations={mutations} onCreated={setResult} />
        ) : (
          <InviteResultView
            result={result}
            onDone={() => {
              onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

/**
 * The link the dialog is showing, cleared as the dialog opens again so a previous invitation's link
 * is never shown twice.
 */
export function useResettingResult(
  open: boolean,
): [InviteResult | null, (result: InviteResult) => void] {
  const [result, setResult] = useState<InviteResult | null>(null);
  const [wasOpen, setWasOpen] = useState(open);

  if (wasOpen !== open) {
    setWasOpen(open);

    if (open) {
      setResult(null);
    }
  }

  return [result, setResult];
}

function InviteForm({
  me,
  mutations,
  onCreated,
}: {
  readonly me: Me;
  readonly mutations: Mutations;
  readonly onCreated: (result: InviteResult) => void;
}): ReactElement {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<UserRole>("member");
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [expiry, setExpiry] = useState<InviteExpiry>(defaultInviteExpiry);
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const { create } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    create.mutate(
      {
        body: {
          email: email.trim(),
          role,
          expiry,
          ...(groupIds.length === 0 ? {} : { groupIds }),
        },
      },
      { onSuccess: onCreated },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Email"
        type="email"
        value={email}
        spellCheck={false}
        autoComplete="off"
        placeholder="alice@example.com"
        description="Where the invitation goes, and the address the sign-in must prove."
        onChange={(event) => {
          setEmail(event.target.value);
        }}
      />
      <Select
        className="w-full"
        label="Role"
        value={role}
        renderValue={(value) => roleName(value ?? "")}
        onValueChange={(value: UserRole | null) => {
          if (value !== null) {
            setRole(value);
          }
        }}
      >
        {invitableRoles.map((option) => (
          <Select.Option key={option.value} value={option.value}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{option.label}</span>
              <span className="text-sm text-kumo-subtle">{option.description}</span>
            </span>
          </Select.Option>
        ))}
      </Select>
      {groups.data === undefined ? null : (
        <MultiPicker
          label="Groups"
          description="The account joins these groups the moment it is created."
          placeholder="No groups"
          items={groupItems(groups.data.groups, "membership")}
          value={groupIds}
          onValueChange={setGroupIds}
        />
      )}
      <Select
        className="w-full"
        label="Link expires after"
        value={expiry}
        items={[...inviteExpiryOptions]}
        onValueChange={(value: string | null) => {
          setExpiry(toInviteExpiry(value));
        }}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button
          type="submit"
          variant="primary"
          loading={create.isPending}
          disabled={email.trim() === ""}
        >
          Send invite
        </Button>
      </DialogFooter>
    </form>
  );
}

/** The link an invitation carries, with what became of the email. Shown once. */
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
      <div className="rounded-lg bg-kumo-recessed p-3 ring ring-kumo-line">
        {/* A button centres its text by default, and a wrapped link reads as a column of fragments
            unless it starts at the left edge with the icon on its first line. */}
        <CopyText
          value={result.url}
          wrap
          label="Copy invitation link"
          className="max-w-none items-start text-left"
        />
      </div>
      <MailOutcome result={result} />
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
      <Callout tone="warning" title="The email was not sent" description={result.emailError} />
    );
  }

  return (
    <p className="text-kumo-subtle">
      This server sends no mail, so the link has to reach the person another way.
    </p>
  );
}
