import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useQuery } from "@tanstack/react-query";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import { usersQuery } from "~/api/queries.ts";
import type { User } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import { scopeItems } from "~/components/keys/scopes.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { userLabel } from "~/lib/node.ts";

const revealNote = "The full key is shown this once and cannot be read again.";

/** The owner, an admin and the socket may mint a key for someone else. */
function managesAllKeys(me: Me): boolean {
  return me.allAccess || me.role === "owner" || me.role === "admin";
}

export function CreateApiKeyDialog({
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
        title={created === null ? "Create API key" : "API key created"}
        description={
          created === null
            ? "The key authenticates calls to the headscale API with the permissions of its owner, or with the scopes you pick."
            : undefined
        }
      >
        {created === null ? (
          <CreateApiKeyForm me={me} onCreated={setCreated} />
        ) : (
          <CreatedKey value={created} note={revealNote} onDone={close} />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

function CreateApiKeyForm({
  me,
  onCreated,
}: {
  readonly me: Me;
  readonly onCreated: (key: string) => void;
}): ReactElement {
  const forOthers = managesAllKeys(me) && can(me, "users:read");
  const users = useQuery({ ...usersQuery, enabled: forOthers });
  const { create } = useApiKeyMutations();
  const [expiry, setExpiry] = useState<ExpiryChoice>("90d");
  const [userId, setUserId] = useState(me.user?.id ?? "");
  const [description, setDescription] = useState("");
  const [scopes, setScopes] = useState<readonly string[]>([]);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    const body = {
      expiration: expirationFor(expiry),
      description: description.trim(),
      scopes: [...scopes],
      ...(userId === "" ? {} : { userId }),
    };
    create.mutate(
      { body },
      {
        onSuccess: (data) => {
          onCreated(data.apiKey);
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
        placeholder="CI deploy, monitoring…"
        onChange={(event) => {
          setDescription(event.target.value);
        }}
      />
      <Select
        className="w-full"
        label="Expiration"
        value={expiry}
        items={expiryOptions}
        onValueChange={(value: ExpiryChoice | null) => {
          if (value !== null) {
            setExpiry(value);
          }
        }}
      />
      {forOthers ? (
        <Select
          className="w-full"
          label="For user"
          description="The key acts with this user's role."
          value={userId}
          items={userOptions(users.data?.users)}
          onValueChange={(value: string | null) => {
            setUserId(value ?? "");
          }}
        />
      ) : null}
      <MultiPicker
        label="Scopes"
        description="Limit the key to these operations. Empty means everything its owner may do. A scope the owner lacks is dropped."
        placeholder="Everything the owner may do"
        items={scopeItems(me)}
        value={scopes}
        onValueChange={setScopes}
        empty="No scope matches."
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" loading={create.isPending}>
          Create key
        </Button>
      </DialogFooter>
    </form>
  );
}

function userOptions(users: readonly User[] | undefined): { value: string; label: string }[] {
  return [
    { value: "", label: "No user (all access)" },
    ...(users ?? []).map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}
