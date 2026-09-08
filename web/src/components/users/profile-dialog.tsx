import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { User } from "~/api/queries.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import type { UserDialogProps } from "~/components/users/dialogs.tsx";
import type { useUserMutations } from "~/components/users/mutations.ts";
import { userLabel } from "~/lib/node.ts";

type Mutations = ReturnType<typeof useUserMutations>;

export function EditProfileDialog({
  user,
  open,
  onOpenChange,
  mutations,
}: UserDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={`Edit profile for ${user.name}`}
        description="What the clients show for this user. A user who logs in through an identity provider gets these from the provider again at the next login."
      >
        <EditProfileForm user={user} mutations={mutations} onOpenChange={onOpenChange} />
      </DialogContent>
    </DialogRoot>
  );
}

function EditProfileForm({
  user,
  mutations,
  onOpenChange,
}: {
  readonly user: User;
  readonly mutations: Mutations;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [displayName, setDisplayName] = useState(user.displayName);
  const [email, setEmail] = useState(user.email);
  const [pictureUrl, setPictureUrl] = useState(user.profilePicUrl);
  const [pictureTouched, setPictureTouched] = useState(false);
  const { update } = mutations;
  const unchanged =
    displayName.trim() === user.displayName &&
    email.trim() === user.email &&
    pictureUrl.trim() === user.profilePicUrl;
  const pictureError = pictureUrlError(pictureUrl.trim());

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    if (pictureError !== undefined) {
      setPictureTouched(true);

      return;
    }

    update.mutate(
      {
        params: { path: { id: user.id } },
        body: {
          displayName: displayName.trim(),
          email: email.trim(),
          pictureUrl: pictureUrl.trim(),
        },
      },
      {
        onSuccess: () => {
          toast.success("Profile updated");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Display name"
        description="Shown instead of the username. Leave empty to show the username."
        required={false}
        value={displayName}
        autoComplete="off"
        onChange={(event) => {
          setDisplayName(event.target.value);
        }}
      />
      <Input
        label="Email"
        required={false}
        type="email"
        value={email}
        autoComplete="off"
        onChange={(event) => {
          setEmail(event.target.value);
        }}
      />
      <Input
        label="Picture URL"
        description="An https URL of the picture the clients show."
        required={false}
        type="url"
        value={pictureUrl}
        spellCheck={false}
        autoComplete="off"
        placeholder="https://example.com/avatar.png"
        {...(pictureTouched && pictureError !== undefined ? { error: pictureError } : {})}
        onBlur={() => {
          setPictureTouched(true);
        }}
        onChange={(event) => {
          setPictureUrl(event.target.value);
        }}
      />
      <PicturePreview
        url={pictureError === undefined ? pictureUrl.trim() : ""}
        name={userLabel(user)}
      />
      <DialogError message={update.isError ? errorMessage(update.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button type="submit" variant="primary" loading={update.isPending} disabled={unchanged}>
          Save
        </Button>
      </DialogFooter>
    </form>
  );
}

/** Why a picture URL cannot be saved, or undefined when it is empty or an https URL with a host. */
function pictureUrlError(url: string): string | undefined {
  if (url === "") {
    return undefined;
  }

  let parsed: URL;

  try {
    parsed = new URL(url);
  } catch {
    return "Enter a full URL, starting with https://.";
  }

  if (parsed.protocol !== "https:" || parsed.host === "") {
    return "The picture must be served over https.";
  }

  return undefined;
}

/**
 * The picture as the clients would show it, so a URL that answers with a missing image is caught
 * before Save. A failure is advisory: the URL may be reachable only from the tailnet.
 */
function PicturePreview({
  url,
  name,
}: {
  readonly url: string;
  readonly name: string;
}): ReactElement | null {
  const [failed, setFailed] = useState<string | null>(null);

  if (url === "") {
    return null;
  }

  return (
    <div className="flex items-center gap-3 text-sm text-kumo-subtle">
      {failed === url ? (
        <>
          <Avatar name={name} size="lg" />
          <span>The picture did not load from here; it may still load from the clients.</span>
        </>
      ) : (
        <>
          <img
            src={url}
            alt=""
            className="size-8 rounded-md object-cover ring ring-kumo-line"
            onError={() => {
              setFailed(url);
            }}
          />
          <span>Preview</span>
        </>
      )}
    </div>
  );
}
