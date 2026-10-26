import { LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { needsTags, scopeItems } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import {
  FormBar,
  FormLayout,
  FormPage,
  FormSection,
  nameFieldClass,
} from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

const listPath = "/keys/oauth";

/**
 * Creating an OAuth client on a page of its own, laid out like the dashboard's "Create API token":
 * its name, the scopes its tokens carry, and the tags it may hand out, then the save bar with
 * Cancel and the action. The secret is shown on the same page once the client exists.
 */
export function CreateOAuthClientPage({ me }: { readonly me: Me }): ReactElement {
  const [created, setCreated] = useState<string | null>(null);

  return created === null ? (
    <FormLayout>
      <PageHeader
        title="Create OAuth client"
        description="The client exchanges its secret for short-lived v2 API tokens, limited to the scopes you pick."
      />
      <OAuthClientForm me={me} onCreated={setCreated} />
    </FormLayout>
  ) : (
    <FormLayout>
      <PageHeader
        title="OAuth client created"
        description="Copy the secret before you leave this page. It is not shown again."
      />
      <FormSection title="Client secret">
        <RevealedSecret
          noun="secret"
          value={created}
          note="The secret is shown only once. Copy it now."
        />
      </FormSection>
      <FormBar>
        <LinkButton href={listPath} variant="primary">
          Done
        </LinkButton>
      </FormBar>
    </FormLayout>
  );
}

function OAuthClientForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (secret: string) => void;
}): ReactElement {
  const { create } = useOAuthClientMutations();
  const [description, setDescription] = useState("");
  const [scopes, setScopes] = useState<readonly string[]>([]);
  const [tags, setTags] = useState<readonly string[]>([]);
  const knownTags = useKnownTags(me);
  const [tried, setTried] = useState(false);
  const scopesIssue = scopes.length === 0 ? "Pick at least one scope." : undefined;
  const tagsIssue =
    needsTags(scopes) && tags.length === 0
      ? "Add a tag: the client's machines and keys are owned by it."
      : undefined;

  const edited = (): void => {
    if (create.isError) {
      create.reset();
    }
  };

  function submit(): boolean {
    setTried(true);

    if (scopesIssue !== undefined || tagsIssue !== undefined) {
      return false;
    }

    create.mutate(
      { body: { description: description.trim(), scopes: [...scopes], tags: [...tags] } },
      {
        onSuccess: (data) => {
          onCreated(data.clientSecret ?? "");
        },
      },
    );

    return true;
  }

  return (
    <FormPage
      noun="client"
      creating
      canEdit
      error={create.isError ? errorMessage(create.error) : undefined}
      errorTitle="The client was not created"
      pending={create.isPending}
      dirty={description !== "" || scopes.length > 0 || tags.length > 0}
      onSubmit={submit}
      cancelTo={listPath}
    >
      <FormSection
        title="Client description"
        info="What the client is for, so it can be told apart."
      >
        <Input
          aria-label="Client description"
          className={nameFieldClass}
          value={description}
          placeholder="Terraform, CI deploys…"
          onChange={(event) => {
            setDescription(event.target.value);
            edited();
          }}
        />
      </FormSection>
      <FormSection
        title="Scopes"
        info="What the client's tokens may do. You can only grant scopes you hold."
      >
        <MultiPicker
          label="Scopes"
          description="Pick at least one."
          placeholder="Pick scopes"
          items={scopeItems(me)}
          value={scopes}
          onValueChange={(value) => {
            setScopes(value);
            edited();
          }}
          empty="No scope matches."
          error={tried ? scopesIssue : undefined}
        />
      </FormSection>
      <FormSection
        title="Tags"
        info="Machines and pre-auth keys the client creates are owned by these tags."
      >
        <TagField
          required={needsTags(scopes)}
          description={
            needsTags(scopes)
              ? "Machine and pre-auth key scopes need tags. Everything the client creates is owned by them."
              : "Tags the client may put on the machines and keys it creates."
          }
          placeholder="tag:ci"
          value={tags}
          suggestions={knownTags}
          onValueChange={(value) => {
            setTags(value);
            edited();
          }}
          error={tried ? tagsIssue : undefined}
        />
      </FormSection>
    </FormPage>
  );
}
