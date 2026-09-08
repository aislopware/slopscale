import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { User } from "~/api/queries.ts";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import type { useUserMutations } from "~/components/users/mutations.ts";
import { roleOptions, toRole } from "~/components/users/roles.ts";
import type { UserRole } from "~/components/users/roles.ts";
import { userLabel } from "~/lib/node.ts";

type Mutations = ReturnType<typeof useUserMutations>;

const nameHint = "Lowercase letters, digits and dashes; it must be unique.";

export interface UserDialogProps {
  readonly user: User;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: Mutations;
}

/** The dialog body only mounts while the dialog is open, so every form starts from the record. */
export function CreateUserDialog({
  open,
  onOpenChange,
  mutations,
}: Omit<UserDialogProps, "user">): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Add user"
        description="A local user that can register machines with a pre-auth key. Users who sign in through an identity provider appear on their own."
      >
        <CreateUserForm mutations={mutations} onOpenChange={onOpenChange} />
      </DialogContent>
    </DialogRoot>
  );
}

function CreateUserForm({
  mutations,
  onOpenChange,
}: {
  readonly mutations: Mutations;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [email, setEmail] = useState("");
  const { create } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    const label = displayName.trim();
    const address = email.trim();
    create.mutate(
      {
        body: {
          name: name.trim(),
          ...(label === "" ? {} : { displayName: label }),
          ...(address === "" ? {} : { email: address }),
        },
      },
      {
        onSuccess: () => {
          toast.success("User created");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Username"
        description={nameHint}
        value={name}
        spellCheck={false}
        autoComplete="off"
        placeholder="alice"
        onChange={(event) => {
          setName(event.target.value);
        }}
      />
      <Input
        label="Display name"
        required={false}
        value={displayName}
        placeholder="Alice Nguyen"
        onChange={(event) => {
          setDisplayName(event.target.value);
        }}
      />
      <Input
        label="Email"
        required={false}
        type="email"
        value={email}
        spellCheck={false}
        autoComplete="off"
        placeholder="alice@example.com"
        onChange={(event) => {
          setEmail(event.target.value);
        }}
      />
      <DialogError message={create.isError ? errorMessage(create.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button
          type="submit"
          variant="primary"
          loading={create.isPending}
          disabled={name.trim() === ""}
        >
          Add user
        </Button>
      </DialogFooter>
    </form>
  );
}

export function RenameUserDialog({
  user,
  open,
  onOpenChange,
  mutations,
}: UserDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Rename user"
        description="The username is what the policy and the machine names refer to, so renaming changes both."
      >
        <RenameUserForm user={user} mutations={mutations} onOpenChange={onOpenChange} />
      </DialogContent>
    </DialogRoot>
  );
}

function RenameUserForm({
  user,
  mutations,
  onOpenChange,
}: {
  readonly user: User;
  readonly mutations: Mutations;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [name, setName] = useState(user.name);
  const { rename } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    rename.mutate(
      { params: { path: { oldId: user.id, newName: name.trim() } } },
      {
        onSuccess: () => {
          toast.success("User renamed");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Username"
        description={nameHint}
        value={name}
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          setName(event.target.value);
        }}
      />
      <DialogError message={rename.isError ? errorMessage(rename.error) : undefined} />
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button
          type="submit"
          variant="primary"
          loading={rename.isPending}
          disabled={name.trim() === "" || name.trim() === user.name}
        >
          Rename
        </Button>
      </DialogFooter>
    </form>
  );
}

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
        title="Edit profile"
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
  const { update } = mutations;
  const unchanged =
    displayName.trim() === user.displayName &&
    email.trim() === user.email &&
    pictureUrl.trim() === user.profilePicUrl;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
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
        description="Shown in place of the username; empty shows the username."
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
        onChange={(event) => {
          setPictureUrl(event.target.value);
        }}
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

export function RoleDialog({ user, open, onOpenChange, mutations }: UserDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={`Change role for ${userLabel(user)}`}
        description="The role decides what this user may do in the console and through the API. There is exactly one owner, so picking owner transfers ownership."
      >
        <RoleForm user={user} mutations={mutations} onOpenChange={onOpenChange} />
      </DialogContent>
    </DialogRoot>
  );
}

function RoleForm({
  user,
  mutations,
  onOpenChange,
}: {
  readonly user: User;
  readonly mutations: Mutations;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [role, setRole] = useState<UserRole>(toRole(user.role));
  const { setRole: mutation } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    mutation.mutate(
      { params: { path: { id: user.id } }, body: { role } },
      {
        onSuccess: () => {
          toast.success("Role changed");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Select
        className="w-full"
        label="Role"
        value={role}
        renderValue={(value) => value}
        onValueChange={(value: UserRole | null) => {
          if (value !== null) {
            setRole(value);
          }
        }}
        {...(mutation.isError ? { error: errorMessage(mutation.error) } : {})}
      >
        {roleOptions.map((option) => (
          <Select.Option key={option.value} value={option.value}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{option.label}</span>
              <span className="text-sm text-kumo-subtle">{option.description}</span>
            </span>
          </Select.Option>
        ))}
      </Select>
      <DialogFooter>
        <DialogClose render={<Button variant="secondary">Cancel</Button>} />
        <Button
          type="submit"
          variant="primary"
          loading={mutation.isPending}
          disabled={role === user.role}
        >
          Change role
        </Button>
      </DialogFooter>
    </form>
  );
}

/**
 * Deleting a user takes their machines and keys with it, so it asks for the username to be typed
 * back rather than a plain yes/no.
 */
export function DeleteUserDialog({
  user,
  open,
  onOpenChange,
  mutations,
}: UserDialogProps): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="user"
      resourceName={user.name}
      deleteButtonText="Delete user"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: user.id } } },
          {
            onSuccess: () => {
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}
