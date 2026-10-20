import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { RevealedSecret } from "~/components/keys/created-key.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { needsTags, scopeItems } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import { FormError, FormFooter, FormSection } from "~/components/ui/form-page.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

const listPath = "/keys/oauth";

/**
 * Creating an OAuth client on a page of its own, laid out like the dashboard's "Create API token":
 * its name, the scopes its tokens carry, and the tags it may hand out, then Cancel on the left and
 * the action on the right. The secret is shown on the same page once the client exists.
 */
export function CreateOAuthClientPage({ me }: { readonly me: Me }): ReactElement {
  const [created, setCreated] = useState<string | null>(null);

  return created === null ? (
    <>
      <PageHeader
        title="Create OAuth client"
        docs="ref/api"
        description="The client exchanges its secret for short-lived v2 API tokens, limited to the scopes you pick."
      />
      <OAuthClientForm me={me} onCreated={setCreated} />
    </>
  ) : (
    <>
      <PageHeader
        title="OAuth client created"
        docs="ref/api"
        description="Copy the secret before you leave this page. It is not shown again."
      />
      <div className="flex flex-col gap-4">
        <FormSection title="Client secret">
          <RevealedSecret value={created} note="The secret is shown only once. Copy it now." />
        </FormSection>
        <FormFooter>
          <LinkButton href={listPath} variant="primary">
            Done
          </LinkButton>
        </FormFooter>
      </div>
    </>
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
  const canSubmit = scopes.length > 0 && (!needsTags(scopes) || tags.length > 0);

  const edited = (): void => {
    if (create.isError) {
      create.reset();
    }
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the button says, so the guard is here too.
    if (!canSubmit || create.isPending) {
      return;
    }

    create.mutate(
      { body: { description: description.trim(), scopes: [...scopes], tags: [...tags] } },
      {
        onSuccess: (data) => {
          onCreated(data.clientSecret ?? "");
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <FormError
        title="The client was not created"
        message={create.isError ? errorMessage(create.error) : undefined}
      />
      <FormSection
        title="Client description"
        info="What the client is for, so it can be told apart."
      >
        <Input
          aria-label="Client description"
          className="max-w-[450px]"
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
        />
      </FormSection>
      <FormFooter cancelTo={listPath}>
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={create.isPending}>
          Create client
        </Button>
      </FormFooter>
    </form>
  );
}
