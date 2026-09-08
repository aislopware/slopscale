import { Button } from "@cloudflare/kumo/components/button";
import { Input, InputArea } from "@cloudflare/kumo/components/input";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Me } from "~/auth/me.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { useOAuthClientMutations } from "~/components/keys/mutations.ts";
import { parseTags } from "~/components/keys/preauth-dialogs.tsx";
import { scopeItems } from "~/components/keys/scopes.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";

const revealNote = "The client secret is shown this once and cannot be read again.";
const tagRows = 2;

/** Scopes that mint machine credentials, which the server only allows tagged. */
const taggedScopes: ReadonlySet<string> = new Set(["devices:core", "auth_keys", "all"]);

function needsTags(scopes: readonly string[]): boolean {
  return scopes.some((scope) => taggedScopes.has(scope));
}

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
            ? "A client mints short-lived tokens for the v2 API with its secret, within the scopes you pick."
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
  const [tags, setTags] = useState("");

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    create.mutate(
      { body: { description: description.trim(), scopes: [...scopes], tags: parseTags(tags) } },
      {
        onSuccess: (data) => {
          onCreated(data.clientSecret);
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
        description="What the client's tokens may do; at least one, and none beyond your own."
        placeholder="Pick scopes"
        items={scopeItems(me)}
        value={scopes}
        onValueChange={setScopes}
        empty="No scope matches."
      />
      <InputArea
        label="Tags"
        required={needsTags(scopes)}
        description={
          needsTags(scopes)
            ? "Required with machine or pre-auth key scopes: everything the client creates is owned by these tags."
            : "Tags the client may put on the machines and keys it creates."
        }
        value={tags}
        spellCheck={false}
        placeholder="tag:ci, tag:server"
        minRows={tagRows}
        onValueChange={setTags}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button
          type="submit"
          variant="primary"
          disabled={scopes.length === 0}
          loading={create.isPending}
        >
          Create client
        </Button>
      </DialogFooter>
    </form>
  );
}
