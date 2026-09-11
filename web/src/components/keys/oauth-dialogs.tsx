import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { needsTags, scopeItems } from "~/components/keys/scopes.ts";
import { useKnownTags } from "~/components/tags/use-known-tags.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { TagField } from "~/components/ui/tag-field.tsx";

export { CreateFederatedIdentityDialog } from "~/components/keys/federated-dialogs.tsx";
export { EditOAuthClientDialog } from "~/components/keys/edit-oauth-dialog.tsx";

const revealNote = "The secret is shown only once. Copy it now.";

export function CreateOAuthClientDialog({
  me,
  open,
  onOpenChange,
}: {
  readonly me: Me;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [created, setCreated] = useCreatedKey(open);
  const close = (): void => {
    onOpenChange(false);
  };

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={created === null ? "Create OAuth client" : "OAuth client created"}
        description={
          created === null
            ? "The client exchanges its secret for short-lived v2 API tokens, limited to the scopes you pick."
            : undefined
        }
      >
        {created === null ? (
          <CreateOAuthClientForm me={me} onCreated={setCreated} />
        ) : (
          <CreatedKey value={created} note={revealNote} onDone={close} />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

function CreateOAuthClientForm({
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

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    // Enter in a text input submits the form whatever the buttons say, so the guard is here too.
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
      <Input
        label="Description"
        required={false}
        value={description}
        placeholder="Terraform, CI deploys…"
        onChange={(event) => {
          setDescription(event.target.value);
        }}
      />
      <MultiPicker
        label="Scopes"
        description="What the client's tokens may do. You can only grant scopes you hold."
        placeholder="Pick scopes"
        items={scopeItems(me)}
        value={scopes}
        onValueChange={setScopes}
        empty="No scope matches."
      />
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
        onValueChange={setTags}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter submitDisabled={!canSubmit || create.isPending}>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={create.isPending}>
          Create client
        </Button>
      </DialogFooter>
    </form>
  );
}
