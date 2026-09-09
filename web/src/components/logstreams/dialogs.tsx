import { DeleteResource } from "@cloudflare/kumo";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { LogStream } from "~/api/queries.ts";
import {
  destinationOption,
  destinationOptions,
  toDestination,
  urlError,
} from "~/components/logstreams/model.ts";
import type { Destination, DestinationOption } from "~/components/logstreams/model.ts";
import type { LogStreamMutations } from "~/components/logstreams/mutations.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface LogStreamDialogProps {
  /** The stream to edit; absent when creating one. */
  readonly stream?: LogStream | undefined;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: LogStreamMutations;
}

/** Creates or edits a log stream. The form mounts with the dialog. */
export function LogStreamDialog(props: LogStreamDialogProps): ReactElement {
  const editing = props.stream !== undefined;

  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        size="lg"
        title={editing ? "Edit log stream" : "New log stream"}
        description="Every audit log entry is posted to the destination in batches, in the shape it expects."
      >
        <LogStreamForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

interface Draft {
  readonly name: string;
  readonly destination: Destination;
  readonly url: string;
  readonly token: string;
  readonly enabled: boolean;
}

function draftFrom(stream: LogStream | undefined): Draft {
  return {
    name: stream?.name ?? "",
    destination: toDestination(stream?.destination ?? "http"),
    url: stream?.url ?? "",
    token: "",
    enabled: stream?.enabled ?? true,
  };
}

/** Why the form cannot be sent yet, or null when it can. */
function draftIssue(draft: Draft, editing: boolean, hasToken: boolean): string | null {
  if (draft.name.trim() === "" || draft.url.trim() === "") {
    return "incomplete";
  }

  const option = destinationOption(draft.destination);

  if (option.tokenRequired && draft.token.trim() === "" && !(editing && hasToken)) {
    return "token";
  }

  return urlError(draft.url);
}

function LogStreamForm({
  stream,
  onOpenChange,
  mutations,
}: Omit<LogStreamDialogProps, "open">): ReactElement {
  const [draft, setDraft] = useState<Draft>(() => draftFrom(stream));
  const [touched, setTouched] = useState(false);
  const editing = stream !== undefined;
  const mutation = editing ? mutations.update : mutations.create;
  const option = destinationOption(draft.destination);
  const urlIssue = urlError(draft.url);
  const update = (patch: Partial<Draft>): void => {
    setDraft((current) => ({ ...current, ...patch }));
  };

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    const body = {
      name: draft.name.trim(),
      destination: draft.destination,
      url: draft.url.trim(),
      token: draft.token.trim(),
      enabled: draft.enabled,
    };

    if (stream === undefined) {
      mutations.create.mutate(
        { body },
        {
          onSuccess: () => {
            toast.success("Log stream created");
            onOpenChange(false);
          },
        },
      );
    } else {
      mutations.update.mutate(
        { params: { path: { id: stream.id } }, body },
        {
          onSuccess: () => {
            toast.success("Log stream updated");
            onOpenChange(false);
          },
        },
      );
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Name"
        value={draft.name}
        placeholder="SIEM"
        onChange={(event) => {
          update({ name: event.target.value });
        }}
      />
      <Select
        className="w-full"
        label="Destination"
        value={draft.destination}
        renderValue={(value: Destination | null) =>
          value === null ? "" : destinationOption(value).label
        }
        onValueChange={(value: Destination | null) => {
          if (value !== null) {
            update({ destination: value });
          }
        }}
      >
        {destinationOptions.map((item) => (
          <Select.Option key={item.value} value={item.value}>
            <span className="flex flex-col gap-0.5">
              <span className="font-medium text-kumo-default">{item.label}</span>
              <span className="text-sm text-kumo-subtle">{item.description}</span>
            </span>
          </Select.Option>
        ))}
      </Select>
      <Input
        label="URL"
        type="url"
        value={draft.url}
        spellCheck={false}
        autoComplete="off"
        placeholder={option.placeholder}
        onChange={(event) => {
          update({ url: event.target.value });
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && urlIssue !== null ? { error: urlIssue } : {})}
      />
      <TokenField
        option={option}
        editing={editing}
        hasToken={stream?.hasToken ?? false}
        value={draft.token}
        onChange={(token) => {
          update({ token });
        }}
      />
      <EnabledSwitch
        checked={draft.enabled}
        onChange={(enabled) => {
          update({ enabled });
        }}
      />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter
        label={editing ? "Save" : "Create log stream"}
        pending={mutation.isPending}
        disabled={draftIssue(draft, editing, stream?.hasToken ?? false) !== null}
      />
    </form>
  );
}

function TokenField({
  option,
  editing,
  hasToken,
  value,
  onChange,
}: {
  readonly option: DestinationOption;
  readonly editing: boolean;
  readonly hasToken: boolean;
  readonly value: string;
  readonly onChange: (value: string) => void;
}): ReactElement {
  const keeps = editing && hasToken;

  return (
    <Input
      label={option.tokenLabel}
      type="password"
      required={option.tokenRequired && !keeps}
      value={value}
      autoComplete="off"
      placeholder={keeps ? "Unchanged" : ""}
      description={
        editing
          ? "Leave empty to keep the stored credential."
          : "Stored on the server and never shown again."
      }
      onChange={(event) => {
        onChange(event.target.value);
      }}
    />
  );
}

function EnabledSwitch({
  checked,
  onChange,
}: {
  readonly checked: boolean;
  readonly onChange: (checked: boolean) => void;
}): ReactElement {
  return (
    <Switch.Group>
      <Switch.Legend>Shipping</Switch.Legend>
      <Switch
        checked={checked}
        onCheckedChange={onChange}
        label={
          <span className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">Enabled</span>
            <span className="text-xs text-kumo-subtle">
              A disabled stream keeps its settings and counters and ships nothing.
            </span>
          </span>
        }
      />
    </Switch.Group>
  );
}

export function DeleteLogStreamDialog({
  stream,
  open,
  onOpenChange,
  mutations,
}: {
  readonly stream: LogStream;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: LogStreamMutations;
}): ReactElement {
  const { remove } = mutations;

  return (
    <DeleteResource
      open={open}
      onOpenChange={onOpenChange}
      resourceType="log stream"
      resourceName={stream.name}
      deleteButtonText="Delete log stream"
      isDeleting={remove.isPending}
      {...(remove.isError ? { errorMessage: errorMessage(remove.error) } : {})}
      onDelete={() => {
        remove.mutate(
          { params: { path: { id: stream.id } } },
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
