import { Input } from "@cloudflare/kumo/components/input";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import {
  FieldGrid,
  FormLayout,
  FormPage,
  FormSection,
  nameFieldClass,
  useDraft,
} from "~/components/ui/form-page.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { toast } from "~/components/ui/toast.ts";
import { emailIssue } from "~/components/users/invites.ts";
import { useUserMutations } from "~/components/users/mutations.ts";

const listPath = "/users";

interface Draft {
  readonly name: string;
  readonly displayName: string;
  readonly email: string;
}

const emptyDraft: Draft = { name: "", displayName: "", email: "" };

/**
 * Adding a local user on a page of its own: the username, and the name and address the console
 * shows for it. Saving goes back to the users list, where the new row is.
 */
export function AddUserPage({ canEdit }: { readonly canEdit: boolean }): ReactElement {
  const navigate = useNavigate();
  const { create } = useUserMutations();
  const { draft, dirty, submitted, update, markSubmitted } = useDraft(emptyDraft, create);
  // The address says what is wrong with it once it has been left, like the dashboard's format checks.
  const [emailLeft, setEmailLeft] = useState(false);
  const nameIssue = draft.name.trim() === "" ? "Give the user a username." : undefined;
  const addressIssue =
    draft.email.trim() === "" ? undefined : (emailIssue(draft.email.trim()) ?? undefined);

  function save(): boolean {
    markSubmitted();
    setEmailLeft(true);

    if (nameIssue !== undefined || addressIssue !== undefined) {
      return false;
    }

    const label = draft.displayName.trim();
    const address = draft.email.trim();

    create.mutate(
      {
        body: {
          name: draft.name.trim(),
          ...(label === "" ? {} : { displayName: label }),
          ...(address === "" ? {} : { email: address }),
        },
      },
      {
        onSuccess: () => {
          toast.success("User created");
          void navigate({ to: listPath, ignoreBlocker: true });
        },
      },
    );

    return true;
  }

  return (
    <FormLayout>
      <PageHeader
        title="Add user"
        description="A local user that registers machines with a pre-auth key. Users from the identity provider appear on their own after their first sign-in."
      />
      <FormPage
        noun="user"
        creating
        canEdit={canEdit}
        error={create.isError ? errorMessage(create.error) : undefined}
        errorTitle="The user was not created"
        pending={create.isPending}
        dirty={dirty}
        onSubmit={save}
        cancelTo={listPath}
        submitLabel="Add user"
      >
        <FormSection
          title="User details"
          info="The policy and machine names use the username. The display name and email only label the user in the console."
        >
          <Input
            label="Username"
            description="Lowercase letters, digits and dashes. It must be unique."
            className={nameFieldClass}
            value={draft.name}
            spellCheck={false}
            autoComplete="off"
            placeholder="alice"
            {...fieldError(submitted ? nameIssue : undefined)}
            onChange={(event) => {
              update({ name: event.target.value });
            }}
          />
          <FieldGrid>
            <Input
              label="Display name"
              required={false}
              value={draft.displayName}
              placeholder="Alice Nguyen"
              onChange={(event) => {
                update({ displayName: event.target.value });
              }}
            />
            <Input
              label="Email"
              required={false}
              type="email"
              value={draft.email}
              spellCheck={false}
              autoComplete="off"
              placeholder="alice@example.com"
              {...fieldError(emailLeft ? addressIssue : undefined)}
              onChange={(event) => {
                update({ email: event.target.value });
              }}
              onBlur={() => {
                setEmailLeft(true);
              }}
            />
          </FieldGrid>
        </FormSection>
      </FormPage>
    </FormLayout>
  );
}
