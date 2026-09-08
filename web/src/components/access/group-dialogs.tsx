import { DeleteResource } from "@cloudflare/kumo";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRule, Group, Node, User } from "~/api/queries.ts";
import { isSynced, rulesUsingGroup } from "~/components/access/model.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { nodeItems, userItems } from "~/components/access/pickers.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { MultiPicker } from "~/components/ui/multi-picker.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface GroupDialogProps {
  /** The group to edit; absent when creating one. */
  readonly group?: Group | undefined;
  readonly nodes: readonly Node[];
  readonly users: readonly User[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}

const nameHint = "Letters, digits, spaces, dots, dashes and underscores; it must be unique.";

/** Creates a group or edits one; the form mounts with the dialog so it starts from the record. */
export function GroupDialog(props: GroupDialogProps): ReactElement {
  const editing = props.group !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit group" : "New group"}
        description="A group is a set of machines. Add machines directly, or add users so every machine they own is in, now and later."
      >
        <GroupForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

function GroupForm({
  group,
  nodes,
  users,
  onOpenChange,
  mutations,
}: Omit<GroupDialogProps, "open">): ReactElement {
  const [name, setName] = useState(group?.name ?? "");
  const [description, setDescription] = useState(group?.description ?? "");
  const [nodeIds, setNodeIds] = useState<string[]>(group?.nodeIds ?? []);
  const [userIds, setUserIds] = useState<string[]>(group?.userIds ?? []);
  const synced = group !== undefined && isSynced(group);
  const [requestable, setRequestable] = useState(group?.requestable ?? false);
  const mutation = group === undefined ? mutations.createGroup : mutations.updateGroup;
  // A synced group's users follow the identity provider, so the request leaves them out rather
  // than resubmitting a list a sign-in may have changed since the dialog opened.
  const body = {
    name: name.trim(),
    description: description.trim(),
    nodeIds,
    ...(synced ? {} : { userIds }),
    requestable,
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const done = {
      onSuccess: (): void => {
        toast.success(group === undefined ? "Group created" : "Group updated");
        onOpenChange(false);
      },
    };

    if (group === undefined) {
      mutations.createGroup.mutate({ body }, done);
    } else {
      mutations.updateGroup.mutate({ params: { path: { id: group.id } }, body }, done);
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        description={
          synced ? "Synced from the identity provider: the name is the groups claim's." : nameHint
        }
        value={name}
        readOnly={synced}
        spellCheck={false}
        autoComplete="off"
        placeholder="Engineering"
        onChange={(event) => {
          setName(event.target.value);
        }}
      />
      <Input
        label="Description"
        required={false}
        value={description}
        placeholder="Laptops of the engineering team"
        onChange={(event) => {
          setDescription(event.target.value);
        }}
      />
      <MultiPicker
        label="Users"
        description={
          synced
            ? "Synced from the identity provider: users follow its groups claim at each sign-in."
            : "Every machine these users own is a member, including ones they register later."
        }
        placeholder={synced ? "Managed by the identity provider" : "Add users…"}
        items={userItems(users)}
        value={userIds}
        onValueChange={setUserIds}
        disabled={synced}
        empty="No user matches."
      />
      <MultiPicker
        label="Machines"
        description="Machines added on their own, such as tagged servers."
        placeholder="Add machines…"
        items={nodeItems(nodes)}
        value={nodeIds}
        onValueChange={setNodeIds}
        empty="No machine matches."
      />
      <Switch.Group>
        <Switch.Legend>Requests</Switch.Legend>
        <Switch
          checked={requestable}
          onCheckedChange={setRequestable}
          label={
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">Members may request access</span>
              <span className="text-xs text-kumo-subtle">
                A signed-in user can ask to join this group for a while; an approver decides under
                Requests.
              </span>
            </span>
          }
        />
      </Switch.Group>
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={group === undefined ? "Create group" : "Save"}
        pending={mutation.isPending}
        disabled={name.trim() === ""}
      />
    </form>
  );
}

export function DeleteGroupDialog({
  group,
  rules,
  open,
  onOpenChange,
  mutations,
}: {
  readonly group: Group;
  readonly rules: readonly AccessRule[];
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { deleteGroup } = mutations;
  const using = rulesUsingGroup(rules, group);

  // The server refuses to delete a group a rule names, so say which rules instead of asking for
  // a confirmation that could only fail.
  if (using.length > 0) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Group in use"
          description={`${group.name} is named by ${using.length === 1 ? "a rule" : `${using.length} rules`}. Edit or delete them first.`}
        >
          <ul className="flex flex-col gap-1 text-kumo-default">
            {using.map((rule) => (
              <li key={rule.id} className="truncate">
                <Link
                  to="/policy"
                  search={{ q: rule.name }}
                  className="hover:text-kumo-link"
                  onClick={() => {
                    onOpenChange(false);
                  }}
                >
                  {rule.name}
                </Link>
              </li>
            ))}
          </ul>
          <DialogFooter>
            <DialogClose render={<Button variant="secondary">Close</Button>} />
          </DialogFooter>
        </DialogContent>
      </DialogRoot>
    );
  }

  const remove = (): void => {
    deleteGroup.mutate(
      { params: { path: { id: group.id } } },
      {
        onSuccess: () => {
          onOpenChange(false);
        },
      },
    );
  };

  // A synced group comes back at the next sign-in that carries its claim, as a new group without
  // this one's description, machines or rules, so say that instead of promising a lasting delete.
  if (isSynced(group)) {
    return (
      <DialogRoot open={open} onOpenChange={onOpenChange}>
        <DialogContent
          size="sm"
          title="Delete synced group"
          description={`${group.name} is synced from the identity provider. The next sign-in whose groups claim names it creates the group again, empty and without this description and these machines. To keep it gone, remove the claim at the provider or turn group sync off.`}
        >
          <DialogError
            message={deleteGroup.isError ? errorMessage(deleteGroup.error) : undefined}
          />
          <DialogFooter>
            <DialogClose render={<Button variant="secondary">Cancel</Button>} />
            <Button variant="destructive" loading={deleteGroup.isPending} onClick={remove}>
              Delete group
            </Button>
          </DialogFooter>
        </DialogContent>
      </DialogRoot>
    );
  }

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="group"
      resourceName={group.name}
      deleteButtonText="Delete group"
      isDeleting={deleteGroup.isPending}
      {...(deleteGroup.isError ? { errorMessage: errorMessage(deleteGroup.error) } : {})}
      onDelete={remove}
    />
  );
}
