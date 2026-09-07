import { Button } from "@cloudflare/kumo/components/button";
import { Input, InputArea } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { Node } from "~/api/queries.ts";
import type { useNodeMutations } from "~/components/machines/mutations.ts";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import {
  DialogClose,
  DialogContent,
  DialogError,
  DialogFooter,
  DialogRoot,
} from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { nodeName } from "~/lib/node.ts";

type Mutations = ReturnType<typeof useNodeMutations>;

export interface NodeDialogProps {
  readonly node: Node;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: Mutations;
}

/** The footer every machine form shares: cancel closes, the primary button submits. */
export function FormFooter({
  label,
  pending,
  disabled = false,
}: {
  readonly label: string;
  readonly pending: boolean;
  readonly disabled?: boolean;
}): ReactElement {
  return (
    <DialogFooter>
      <DialogClose render={<Button variant="secondary">Cancel</Button>} />
      <Button type="submit" variant="primary" loading={pending} disabled={disabled}>
        {label}
      </Button>
    </DialogFooter>
  );
}

export function RenameDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Rename machine"
        description="The name becomes the machine's DNS label, so it must be unique and use only letters, digits and dashes."
      >
        <RenameForm node={node} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function RenameForm({
  node,
  onOpenChange,
  mutations,
}: Omit<NodeDialogProps, "open">): ReactElement {
  const [name, setName] = useState(nodeName(node));
  const { rename } = mutations;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    rename.mutate(
      { params: { path: { nodeId: node.id, newName: name.trim() } } },
      {
        onSuccess: () => {
          toast.success("Machine renamed");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={name}
        spellCheck={false}
        onChange={(event) => {
          setName(event.target.value);
        }}
      />
      <DialogError message={rename.isError ? errorMessage(rename.error) : undefined} />
      <FormFooter
        label="Rename"
        pending={rename.isPending}
        disabled={name.trim() === "" || name.trim() === nodeName(node)}
      />
    </form>
  );
}

function parseTags(text: string): string[] {
  return text
    .split(/[\s,]+/u)
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "")
    .map((tag) => (tag.startsWith("tag:") ? tag : `tag:${tag}`));
}

export function TagsDialog({ node, open, onOpenChange, mutations }: NodeDialogProps): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Edit tags"
        description="A tagged machine belongs to its tags instead of a user. Clearing every tag hands it back to the user that registered it."
      >
        <TagsForm node={node} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function TagsForm({ node, onOpenChange, mutations }: Omit<NodeDialogProps, "open">): ReactElement {
  const [text, setText] = useState(node.tags.join("\n"));
  const { setTags } = mutations;
  const tags = parseTags(text);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTags.mutate(
      { params: { path: { nodeId: node.id } }, body: { tags } },
      {
        onSuccess: () => {
          toast.success(tags.length === 0 ? "Tags removed" : "Tags updated");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <InputArea
        label="Tags"
        description={
          <>
            One per line or comma separated; <span className="font-mono text-[0.9em]">tag:</span> is
            added when missing.
          </>
        }
        value={text}
        spellCheck={false}
        placeholder={"tag:server\ntag:prod"}
        rows={4}
        onValueChange={setText}
      />
      <DialogError message={setTags.isError ? errorMessage(setTags.error) : undefined} />
      <FormFooter label="Save tags" pending={setTags.isPending} />
    </form>
  );
}

export function ExpireDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  const { expire } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Expire machine key?"
      description={`${nodeName(node)} will be disconnected until someone signs in on it again.`}
      confirmLabel="Expire key"
      loading={expire.isPending}
      error={expire.isError ? errorMessage(expire.error) : undefined}
      onConfirm={() => {
        expire.mutate(
          { params: { path: { nodeId: node.id } }, body: {} },
          {
            onSuccess: () => {
              toast.success("Machine key expired");
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}

export function DeleteDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  const { remove } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Remove machine?"
      description={`${nodeName(node)} is removed from the tailnet along with its routes and sharing. The device can register again with a new key.`}
      confirmLabel="Remove"
      loading={remove.isPending}
      error={remove.isError ? errorMessage(remove.error) : undefined}
      onConfirm={() => {
        remove.mutate({ params: { path: { nodeId: node.id } } });
      }}
    />
  );
}
