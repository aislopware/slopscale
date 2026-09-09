// DeleteResource is exported from the package root only.
import { DeleteResource } from "@cloudflare/kumo";
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
import { dnsLabelIssue } from "~/lib/dns-label.ts";
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
    // Enter in a text input presses the hidden submitter, so it holds what the visible button does:
    // a form nobody can submit by button is not one Enter can submit either.
    <DialogFooter submitDisabled={disabled || pending}>
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
  const [touched, setTouched] = useState(false);
  const { rename } = mutations;
  const issue = dnsLabelIssue(name.trim());

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

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
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && issue !== null ? { error: issue } : {})}
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
            One per line or comma separated; <span className="font-mono">tag:</span> is added when
            missing.
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

/**
 * Suspending is reversible, so it asks for one click; lifting a suspension needs no dialog at all
 * and is done from the menu directly.
 */
export function SuspendDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  const { suspend } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Suspend machine?"
      description={`${nodeName(node)} keeps its key but loses every peer until you lift the suspension. No sign-in needed afterwards.`}
      confirmLabel="Suspend"
      loading={suspend.isPending}
      error={suspend.isError ? errorMessage(suspend.error) : undefined}
      onConfirm={() => {
        suspend.mutate(
          { params: { path: { nodeId: node.id } }, body: { suspended: true } },
          {
            onSuccess: () => {
              toast.success("Machine suspended");
              onOpenChange(false);
            },
          },
        );
      }}
    />
  );
}

/**
 * Resetting attestation forgets what the key proved without touching the client, so it asks once
 * and says what happens next: the machine keeps its key and proves itself again on its next map
 * request. Until it does, `node:hardwareAttested` is false and any posture that checks it fails.
 */
export function ResetAttestationDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  const { resetAttestation } = mutations;

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Reset hardware attestation?"
      description={`${nodeName(node)} keeps its key. The record of what it proved is cleared, and the next map request it signs starts it again.`}
      confirmLabel="Reset"
      loading={resetAttestation.isPending}
      error={resetAttestation.isError ? errorMessage(resetAttestation.error) : undefined}
      onConfirm={() => {
        resetAttestation.mutate(
          { params: { path: { nodeId: node.id } } },
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

/** What a client does when it takes an update on, which is why both dialogs ask first. */
const updateWarning =
  "The client downloads the release and restarts Tailscale itself, so the machine drops its connections for a moment.";

/**
 * Asks before one machine is told to update its Tailscale client. The update interrupts whatever is
 * running over the tailnet on that machine and the control plane cannot take it back, so it is a
 * question rather than a button that acts.
 */
export function ClientUpdateDialog({
  name,
  open,
  onOpenChange,
  pending,
  onConfirm,
}: {
  readonly name: string;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly pending: boolean;
  readonly onConfirm: () => void;
}): ReactElement {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      destructive={false}
      title="Update the Tailscale client?"
      description={`${name} is asked to update itself. ${updateWarning}`}
      confirmLabel="Update client"
      loading={pending}
      onConfirm={onConfirm}
    />
  );
}

/**
 * The same question for a selection, with the count it would reach and the ticked machines it steps
 * over: an operator who ticked forty rows should see that eleven of them are offline before the
 * requests go out, not afterwards in a list of refusals.
 */
export function BulkClientUpdateDialog({
  summary,
  open,
  onOpenChange,
  pending,
  onConfirm,
}: {
  /** "3 machines will be asked to update. Skipping 1 offline." */
  readonly summary: string;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly pending: boolean;
  readonly onConfirm: () => void;
}): ReactElement {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      destructive={false}
      title="Update the ticked clients?"
      description={`${summary} ${updateWarning}`}
      confirmLabel="Update clients"
      loading={pending}
      onConfirm={onConfirm}
    />
  );
}

/**
 * Removing a machine is not undoable and the tailnet keeps working without it, so it asks for the
 * name to be typed rather than for one more click.
 */
export function DeleteDialog({
  node,
  open,
  onOpenChange,
  mutations,
}: NodeDialogProps): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="machine"
      resourceName={nodeName(node)}
      deleteButtonText="Remove machine"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate({ params: { path: { nodeId: node.id } } });
      }}
    />
  );
}
