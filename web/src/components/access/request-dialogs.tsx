import { Button } from "@cloudflare/kumo/components/button";
import { Input, Textarea } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { durationChoices } from "~/components/access/request-model.ts";
import type { RequestRow } from "~/components/access/request-model.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { ConfirmDialog } from "~/components/ui/confirm-dialog.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { formatDuration } from "~/lib/time.ts";

const noteRows = 2;

/** Grants the request, for what was asked or for another duration, with a note for the requester. */
export function ApproveRequestDialog({
  request,
  open,
  onOpenChange,
  mutations,
}: {
  readonly request: RequestRow;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Approve request"
        description={`${request.userName} asked to join ${request.groupLabel} for ${formatDuration(request.durationSeconds)}${request.nodeId === undefined ? "" : ` with ${request.nodeLabel}`}.`}
      >
        <ApproveForm request={request} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function ApproveForm({
  request,
  onOpenChange,
  mutations,
}: {
  readonly request: RequestRow;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { approveRequest } = mutations;
  const [seconds, setSeconds] = useState(request.durationSeconds);
  const [note, setNote] = useState("");
  const choices = durationChoices.some((choice) => choice.seconds === request.durationSeconds)
    ? durationChoices
    : [
        { seconds: request.durationSeconds, label: formatDuration(request.durationSeconds) },
        ...durationChoices,
      ];

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    approveRequest.mutate(
      {
        params: { path: { id: request.id } },
        body: {
          note: note.trim(),
          ...(seconds === request.durationSeconds ? {} : { durationSeconds: seconds }),
        },
      },
      {
        onSuccess: () => {
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <DurationSelect
        label="Grant for"
        choices={choices}
        value={seconds}
        onValueChange={setSeconds}
        description="Membership ends after this long."
      />
      <Textarea
        label="Note"
        required={false}
        value={note}
        placeholder="Shown to the requester"
        minRows={noteRows}
        onChange={(event) => {
          setNote(event.target.value);
        }}
      />
      <DialogError
        message={approveRequest.isError ? errorMessage(approveRequest.error) : undefined}
      />
      <FormFooter label="Approve" pending={approveRequest.isPending} />
    </form>
  );
}

export function DurationSelect({
  label,
  description,
  choices,
  value,
  onValueChange,
}: {
  readonly label: string;
  readonly description?: string;
  readonly choices: readonly { seconds: number; label: string }[];
  readonly value: number;
  readonly onValueChange: (seconds: number) => void;
}): ReactElement {
  const labelFor = (seconds: number): string =>
    choices.find((choice) => choice.seconds === seconds)?.label ?? formatDuration(seconds);

  return (
    <Select
      className="w-full"
      label={label}
      {...(description === undefined ? {} : { description })}
      value={String(value)}
      onValueChange={(next) => {
        onValueChange(Number(next ?? value));
      }}
      renderValue={(next) => labelFor(Number(next))}
    >
      {choices.map((choice) => (
        <Select.Option key={choice.seconds} value={String(choice.seconds)}>
          {choice.label}
        </Select.Option>
      ))}
    </Select>
  );
}

/** Turns the request down, with a note the requester sees. */
export function DenyRequestDialog({
  request,
  open,
  onOpenChange,
  mutations,
}: {
  readonly request: RequestRow;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { denyRequest } = mutations;
  const [note, setNote] = useState("");

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    denyRequest.mutate(
      { params: { path: { id: request.id } }, body: { note: note.trim() } },
      {
        onSuccess: () => {
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Deny request"
        description={`${request.userName} asked to join ${request.groupLabel}. The request is kept with the outcome.`}
      >
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Input
            label="Note"
            required={false}
            value={note}
            placeholder="The reason, shown to the requester"
            onChange={(event) => {
              setNote(event.target.value);
            }}
          />
          <DialogError
            message={denyRequest.isError ? errorMessage(denyRequest.error) : undefined}
          />
          <div className="flex justify-end gap-3">
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                onOpenChange(false);
              }}
            >
              Cancel
            </Button>
            <Button type="submit" variant="destructive" loading={denyRequest.isPending}>
              Deny
            </Button>
          </div>
        </form>
      </DialogContent>
    </DialogRoot>
  );
}

/** Withdraws a pending request, or removes a decided one from the record. */
export function CancelRequestDialog({
  request,
  open,
  onOpenChange,
  mutations,
}: {
  readonly request: RequestRow;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { cancelRequest } = mutations;
  const pending = request.status === "pending";

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={pending ? "Withdraw request?" : "Delete request?"}
      description={
        pending
          ? `The request to join ${request.groupLabel} is closed without a decision.`
          : `The record of the request to join ${request.groupLabel} is removed. Access already granted is not taken back.`
      }
      confirmLabel={pending ? "Withdraw" : "Delete"}
      loading={cancelRequest.isPending}
      error={cancelRequest.isError ? errorMessage(cancelRequest.error) : undefined}
      onConfirm={() => {
        cancelRequest.mutate(
          { params: { path: { id: request.id } } },
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
