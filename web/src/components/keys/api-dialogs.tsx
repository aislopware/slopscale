import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import type { ReactElement, SubmitEvent } from "react";
import { useState } from "react";

import { errorMessage } from "~/api/error.ts";
import type { User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { CreatedKey, useCreatedKey } from "~/components/keys/created-key.tsx";
import { expirationFor, expiryOptions } from "~/components/keys/expiration.ts";
import type { ExpiryChoice } from "~/components/keys/expiration.ts";
import { useApiKeyMutations } from "~/components/keys/mutations.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { userLabel } from "~/lib/node.ts";

/** The owner, an admin and the socket may mint a key for someone else. */
export function managesAllKeys(me: Me): boolean {
  return me.allAccess || me.role === "owner" || me.role === "admin";
}

/** The rotate dialog's expiry choices; keeping the key's own expiry is the default. */
type RotateExpiry = "keep" | ExpiryChoice;

const keepExpiry = "keep";

const rotateExpiryOptions: readonly { value: RotateExpiry; label: string }[] = [
  { value: keepExpiry, label: "Keep the current expiry" },
  ...expiryOptions,
];

const rotateNote = "The key is shown only once. Copy it now.";

/**
 * Replaces an API key's secret in place. The key keeps its id, owner, scopes and description, so
 * anything referring to it by prefix stays where it is; only the secret changes, and it changes the
 * moment the operator confirms.
 */
export function RotateApiKeyDialog({
  prefix,
  open,
  onOpenChange,
}: {
  readonly prefix: string;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [rotated, setRotated] = useCreatedKey(open);
  const close = (): void => {
    onOpenChange(false);
  };

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={rotated === null ? "Rotate API key" : "API key rotated"}
        description={
          rotated === null
            ? `Rotating stops the current secret of ${prefix} immediately. The key keeps its id, owner, scopes and description.`
            : undefined
        }
      >
        {rotated === null ? (
          <RotateApiKeyForm prefix={prefix} onRotated={setRotated} />
        ) : (
          <CreatedKey value={rotated} note={rotateNote} onDone={close} />
        )}
      </DialogContent>
    </DialogRoot>
  );
}

function RotateApiKeyForm({
  prefix,
  onRotated,
}: {
  readonly prefix: string;
  readonly onRotated: (key: string) => void;
}): ReactElement {
  const { rotate } = useApiKeyMutations();
  const [expiry, setExpiry] = useState<RotateExpiry>(keepExpiry);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    rotate.mutate(
      {
        params: { path: { prefix } },
        body: expiry === keepExpiry ? {} : { expiration: expirationFor(expiry) },
      },
      {
        onSuccess: (data) => {
          onRotated(data.apiKey);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Select
        className="w-full"
        label="Expiration"
        description="Leave this to keep the key's current expiry."
        value={expiry}
        items={[...rotateExpiryOptions]}
        onValueChange={(value: RotateExpiry | null) => {
          if (value !== null) {
            setExpiry(value);
          }
        }}
      />
      <DialogError message={rotate.isError ? errorMessage(rotate.error) : undefined} />
      <DialogFooter>
        <Button type="submit" variant="destructive" loading={rotate.isPending}>
          Rotate key
        </Button>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
      </DialogFooter>
    </form>
  );
}

/** Who an API key may act as: nobody (all access) or one of the users. */
export function userOptions(
  users: readonly User[] | undefined,
): { value: string; label: string }[] {
  return [
    { value: "", label: "No user (all access)" },
    ...(users ?? []).map((user) => ({ value: user.id, label: userLabel(user) })),
  ];
}
