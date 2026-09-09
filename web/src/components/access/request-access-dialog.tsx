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
import { hourSeconds } from "~/lib/time.ts";

const everyMachine = "";
const reasonRows = 2;
const defaultHours = 4;
const defaultSeconds = defaultHours * hourSeconds;

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
        value={groupId}
        onValueChange={(value) => {
          setGroupId(value ?? "");
        }}
        renderValue={(value) => groupLabel(value)}
      >
        {options.groups.map((group) => (
          <Select.Option key={group.id} value={group.id}>
            <span className="flex flex-col">
              <span>{group.name}</span>
              {group.description === undefined || group.description === "" ? null : (
                <span className="text-xs text-kumo-subtle">{group.description}</span>
              )}
            </span>
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
