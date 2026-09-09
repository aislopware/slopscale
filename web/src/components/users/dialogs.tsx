import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { User } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { plural } from "~/components/overview/plural.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import type { useUserMutations } from "~/components/users/mutations.ts";
import { roleName, roleOptions, toRole } from "~/components/users/roles.ts";
import type { UserRole } from "~/components/users/roles.ts";
import { userLabel } from "~/lib/node.ts";

type Mutations = ReturnType<typeof useUserMutations>;

const nameHint = "Lowercase letters, digits and dashes. It must be unique.";

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
        description="A local user that registers machines with a pre-auth key. Identity provider users appear on their own."
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
        description="The policy and machine names both use the username, so renaming changes both."
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

export function RoleDialog({ user, open, onOpenChange, mutations }: UserDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={`Change role for ${userLabel(user)}`}
        description="What this user may do in the console and the API. Picking owner transfers ownership, since there is only one."
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
        renderValue={(value) => roleName(value ?? "")}
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

/**
 * Ends every console session of one user. Nothing else about the account changes, so it is a plain
 * confirmation rather than a delete; it is offered on the operator's own row too, which is how a
 * browser left signed in somewhere else is dropped.
 */
export function EndSessionsDialog({
  user,
  open,
  onOpenChange,
  mutations,
  me,
}: UserDialogProps & { readonly me?: Me }): ReactElement {
  const { endSessions } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Sign ${userLabel(user)} out everywhere?`}
      description="Every browser signed in as this user is signed out on its next request. The account, its machines and its keys are untouched."
      confirmLabel="Sign out everywhere"
      loading={endSessions.isPending}
      {...(endSessions.isError ? { error: errorMessage(endSessions.error) } : {})}
      onConfirm={() => {
        endSessions.mutate(
          { params: { path: { id: user.id } } },
          {
            onSuccess: (data) => {
              if (me?.user !== undefined && me.user.id === user.id) {
                void signOut();

                return;
              }

              toast.success(`${plural(data.ended, "session")} ended`);
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}
