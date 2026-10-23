import { LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Radio } from "@cloudflare/kumo/components/radio";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { errorMessage } from "~/api/error.ts";
import { groupsQuery } from "~/api/queries.ts";
import type { Group } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { groupItems } from "~/components/access/pickers.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import {
  ChoiceChips,
  FormBar,
  FormLayout,
  FormPage,
  FormSection,
  nameFieldClass,
  useDraft,
} from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { InviteLink } from "~/components/users/invite-link.tsx";
import type { InviteResult } from "~/components/users/invite-link.tsx";
import {
  defaultInviteExpiry,
  emailIssue,
  invitableRoles,
  inviteExpiryLabel,
  inviteExpiryOptions,
} from "~/components/users/invites.ts";
import type { InviteExpiry } from "~/components/users/invites.ts";
import { useInviteMutations } from "~/components/users/mutations.ts";
import { roleName } from "~/components/users/roles.ts";
import type { UserRole } from "~/components/users/roles.ts";

const listPath = "/users";

const pageDescription =
  "The account is created on the first sign-in through the link, or by a verified email that matches.";

/**
 * Inviting a user on a page of its own, the dashboard's "Invite members": the address, the role,
 * the groups the account joins and how long the link works, with a sentence saying what will
 * happen. The link is shown on the same page once the invitation exists, since it is shown once.
 */
export function InvitePage({ me }: { readonly me: Me }): ReactElement {
  const [result, setResult] = useState<InviteResult | null>(null);

  if (result === null) {
    return (
      <FormLayout>
        <PageHeader title="Invite a user" docs="ref/roles" description={pageDescription} />
        <InviteForm me={me} onCreated={setResult} />
      </FormLayout>
    );
  }

  return (
    <FormLayout>
      <PageHeader
        title={result.emailSent ? "Invitation sent" : "Invitation created"}
        docs="ref/roles"
        description="Copy the link before you leave this page. It is not shown again; re-send the invitation for a fresh one."
      />
      <FormSection title="Invitation link">
        <InviteLink result={result} />
      </FormSection>
      <FormBar>
        <LinkButton href={listPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}

interface Draft {
  readonly email: string;
  readonly role: UserRole;
  readonly groupIds: readonly string[];
  readonly expiry: InviteExpiry;
}

const emptyDraft: Draft = {
  email: "",
  role: "member",
  groupIds: [],
  expiry: defaultInviteExpiry,
};

type Change = (patch: Partial<Draft>) => void;

function InviteForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (result: InviteResult) => void;
}): ReactElement {
  const { create } = useInviteMutations();
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(emptyDraft, create);
  const groups = useQuery({ ...groupsQuery, enabled: can(me, "policy_file:read") });
  const issue = emailIssue(draft.email.trim()) ?? undefined;

  function save(): boolean {
    markSubmitted();

    if (issue !== undefined) {
      return false;
    }

    create.mutate(
      {
        body: {
          email: draft.email.trim(),
          role: draft.role,
          expiry: draft.expiry,
          ...(draft.groupIds.length === 0 ? {} : { groupIds: [...draft.groupIds] }),
        },
      },
      { onSuccess: onCreated },
    );

    return true;
  }

  return (
    <FormPage
      noun="invitation"
      creating
      canEdit={can(me, "users")}
      error={create.isError ? errorMessage(create.error) : undefined}
      errorTitle="The invitation was not sent"
      pending={create.isPending}
      dirty={dirty}
      onSubmit={save}
      cancelTo={listPath}
      submitLabel="Send invitation"
    >
      <EmailSection
        email={draft.email}
        issue={issue}
        required={submitted && draft.email.trim() === ""}
        onChange={update}
      />
      <FormSection
        title="Permissions"
        info="What the user may do in the console and the API. The policy, not the role, decides what their machines reach."
      >
        <Radio.Group
          appearance="card"
          value={draft.role}
          onValueChange={(role: UserRole) => {
            update({ role });
          }}
        >
          <Radio.Legend className="sr-only">Role</Radio.Legend>
          {invitableRoles.map((option) => (
            <Radio.Item<UserRole>
              key={option.value}
              value={option.value}
              label={option.label}
              description={option.description}
            />
          ))}
        </Radio.Group>
      </FormSection>
      {groups.data === undefined ? null : (
        <FormSection
          title="Groups"
          optional
          info="The account joins these groups when it is created."
        >
          <MultiPicker
            label="Groups"
            placeholder="Search groups…"
            items={groupItems(groups.data.groups, "membership")}
            value={draft.groupIds}
            onValueChange={(groupIds) => {
              update({ groupIds });
            }}
            empty="No group matches."
          />
        </FormSection>
      )}
      <FormSection title="Link expiration" info="After this the link stops working.">
        <ChoiceChips
          label="Link expiration"
          value={draft.expiry}
          options={inviteExpiryOptions}
          onValueChange={(expiry) => {
            update({ expiry });
          }}
        />
      </FormSection>
      <InviteSummary draft={draft} groups={groups.data?.groups} />
    </FormPage>
  );
}

/**
 * The address, which says it is missing only after a save and is checked for its shape once it has
 * been left, then live.
 */
function EmailSection({
  email,
  issue,
  required,
  onChange,
}: {
  readonly email: string;
  /** What is wrong with the address as typed. */
  readonly issue: string | undefined;
  /** Whether a save found the field empty. */
  readonly required: boolean;
  readonly onChange: Change;
}): ReactElement {
  const [left, setLeft] = useState(false);
  let shown: string | undefined;

  if (required) {
    shown = "Enter the address to invite.";
  } else if (left && email.trim() !== "") {
    shown = issue;
  }

  return (
    <FormSection title="Who to invite">
      <Input
        label="Email"
        type="email"
        className={nameFieldClass}
        value={email}
        spellCheck={false}
        autoComplete="off"
        placeholder="alice@example.com"
        description="Where the invitation goes. The sign-in must prove this address."
        {...fieldError(shown)}
        onChange={(event) => {
          onChange({ email: event.target.value });
        }}
        onBlur={() => {
          setLeft(true);
        }}
      />
    </FormSection>
  );
}

/** One fact of the summary sentence, darker than the words around it. */
function Fact({ children }: { readonly children: ReactNode }): ReactElement {
  return <span className="font-medium text-kumo-default">{children}</span>;
}

/** The invitation in one sentence, so what Send will do reads before it is pressed. */
function InviteSummary({
  draft,
  groups,
}: {
  readonly draft: Draft;
  readonly groups: readonly Group[] | undefined;
}): ReactElement {
  const email = draft.email.trim();
  const names = draft.groupIds.map(
    (id) => groups?.find((group) => group.id === id)?.name ?? `group ${id}`,
  );

  return (
    <p aria-live="polite" className="px-1 text-kumo-subtle">
      {email === "" ? "The invited user" : <Fact>{email}</Fact>} joins as{" "}
      <Fact>{roleName(draft.role)}</Fact>
      {names.length === 0 ? null : (
        <>
          {" "}
          in <Fact>{new Intl.ListFormat("en", { type: "conjunction" }).format(names)}</Fact>
        </>
      )}
      . The link works for <Fact>{inviteExpiryLabel(draft.expiry)}</Fact>.
    </p>
  );
}
