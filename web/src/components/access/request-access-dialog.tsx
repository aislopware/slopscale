import { Textarea } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { AccessRequestOptions } from "~/api/queries.ts";
import type { AccessMutations } from "~/components/access/mutations.ts";
import { DurationSelect } from "~/components/access/request-dialogs.tsx";
import { durationChoices } from "~/components/access/request-model.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { minuteSeconds } from "~/lib/time.ts";

const everyMachine = "every";
const reasonRows = 2;
// The shortest choice is the default: access asked for in a hurry is the
// common case, and a request that is too short is extended, not revoked.
const defaultMinutes = 30;
const defaultSeconds = defaultMinutes * minuteSeconds;

/** Files an ask to join a requestable group for a while, for one machine or all of the user's. */
export function RequestAccessDialog({
  options,
  open,
  onOpenChange,
  mutations,
}: {
  readonly options: AccessRequestOptions;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title="Request access"
        description="Ask to join a group for a while. An approver decides, and the access ends on its own."
      >
        <RequestForm options={options} onOpenChange={onOpenChange} mutations={mutations} />
      </DialogContent>
    </DialogRoot>
  );
}

function RequestForm({
  options,
  onOpenChange,
  mutations,
}: {
  readonly options: AccessRequestOptions;
  readonly onOpenChange: (open: boolean) => void;
  readonly mutations: AccessMutations;
}): ReactElement {
  const { createRequest } = mutations;
  const [groupId, setGroupId] = useState(options.groups[0]?.id ?? "");
  const [nodeId, setNodeId] = useState(everyMachine);
  const [seconds, setSeconds] = useState(defaultSeconds);
  const [reason, setReason] = useState("");
  const groupLabel = (id: string): string =>
    options.groups.find((group) => group.id === id)?.name ?? "Choose a group";
  // A group nobody described says nothing, rather than a line of filler.
  const groupDescription = (id: string): string | undefined => {
    const described = options.groups.find((group) => group.id === id)?.description ?? "";

    return described === "" ? undefined : described;
  };
  const nodeLabel = (id: string): string =>
    id === everyMachine
      ? "Every machine you own"
      : (options.nodes.find((node) => node.id === id)?.name ?? id);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    createRequest.mutate(
      {
        body: {
          groupId,
          durationSeconds: seconds,
          reason: reason.trim(),
          ...(nodeId === everyMachine ? {} : { nodeId }),
        },
      },
      {
        onSuccess: () => {
          toast.success("Request sent");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Select
        className="w-full"
        label="Group"
        // Under the field rather than in the option: the popup is only as wide
        // as the trigger, and a sentence in an option pushes it past the dialog.
        description={groupDescription(groupId)}
        value={groupId}
        onValueChange={(value) => {
          setGroupId(value ?? "");
        }}
        renderValue={(value) => groupLabel(value)}
      >
        {options.groups.map((group) => (
          <Select.Option key={group.id} value={group.id}>
            {group.name}
          </Select.Option>
        ))}
      </Select>
      <Select
        className="w-full"
        label="Machine"
        description="One machine, or every machine you own."
        value={nodeId}
        onValueChange={(value) => {
          setNodeId(value ?? everyMachine);
        }}
        renderValue={(value) => nodeLabel(value)}
      >
        <Select.Option value={everyMachine}>Every machine you own</Select.Option>
        {options.nodes.map((node) => (
          <Select.Option key={node.id} value={node.id}>
            {node.name}
          </Select.Option>
        ))}
      </Select>
      <DurationSelect
        label="For"
        choices={durationChoices}
        value={seconds}
        onValueChange={setSeconds}
      />
      <Textarea
        label="Reason"
        description="The approver sees this."
        required={false}
        value={reason}
        placeholder="Why you need access"
        minRows={reasonRows}
        onChange={(event) => {
          setReason(event.target.value);
        }}
      />
      <DialogError
        message={createRequest.isError ? errorMessage(createRequest.error) : undefined}
      />
      <FormFooter
        label="Send request"
        pending={createRequest.isPending}
        disabled={groupId === ""}
      />
    </form>
  );
}
