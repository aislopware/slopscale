import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { api } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { CustomAttribute } from "~/api/schema.gen.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";
import { toLocalInput } from "~/lib/time.ts";

/** A value as the attribute map renders it: lists joined, booleans and numbers as text. */
export function attributeText(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map((item) => attributeText(item)).join(", ");
  }

  if (typeof value === "string") {
    return value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  return value === undefined || value === null ? "" : JSON.stringify(value);
}

export type ValueKind = "string" | "number" | "boolean";

function kindOf(value: unknown): ValueKind {
  if (typeof value === "number") {
    return "number";
  }

  if (typeof value === "boolean") {
    return "boolean";
  }

  return "string";
}

const valueKinds: readonly ValueKind[] = ["string", "number", "boolean"];

const kindLabels: Record<ValueKind, string> = {
  string: "Text",
  number: "Number",
  boolean: "True or false",
};

export function AttributeDialog({
  node,
  attribute,
  open,
  onOpenChange,
}: {
  readonly node: Node;
  readonly attribute: CustomAttribute | null;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="base"
        title={attribute === null ? "Add custom attribute" : `Edit ${attribute.key}`}
        description="The key starts with custom: and the value is text, a number or true/false. An expiry removes the attribute at that time, which is how a temporary grant such as an on-call marker is made."
      >
        <AttributeForm
          key={attribute?.key ?? "new"}
          node={node}
          attribute={attribute}
          onOpenChange={onOpenChange}
        />
      </DialogContent>
    </DialogRoot>
  );
}

function AttributeForm({
  node,
  attribute,
  onOpenChange,
}: {
  readonly node: Node;
  readonly attribute: CustomAttribute | null;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const [key, setKey] = useState(attribute?.key ?? "custom:");
  const [kind, setKind] = useState<ValueKind>(kindOf(attribute?.value));
  const [text, setText] = useState(attribute === null ? "" : attributeText(attribute.value));
  const [expiry, setExpiry] = useState(toLocalInput(attribute?.expiresAt));
  const [comment, setComment] = useState(attribute?.comment ?? "");
  const queryClient = useQueryClient();
  const set = api.useMutation("put", "/api/v1/node/{nodeId}/attributes/{key}", {
    onSuccess: async () => {
      await invalidate(queryClient, "/api/v1/node");
    },
  });

  const value = parseValue(kind, text);
  const valid = key.startsWith("custom:") && key.length > "custom:".length && value !== undefined;

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();

    if (value === undefined) {
      return;
    }

    set.mutate(
      {
        params: { path: { nodeId: node.id, key: key.trim() } },
        body: {
          value,
          ...(expiry === "" ? {} : { expiry: new Date(expiry).toISOString() }),
          ...(comment.trim() === "" ? {} : { comment: comment.trim() }),
        },
      },
      {
        onSuccess: () => {
          toast.success(attribute === null ? "Attribute added" : "Attribute updated");
          onOpenChange(false);
        },
      },
    );
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label="Key"
        value={key}
        disabled={attribute !== null}
        spellCheck={false}
        placeholder="custom:oncall"
        onChange={(event) => {
          setKey(event.target.value);
        }}
      />
      <div className="grid gap-4 sm:grid-cols-[10rem_minmax(0,1fr)]">
        <Select
          label="Type"
          value={kind}
          renderValue={(current: ValueKind | null) => (current === null ? "" : kindLabels[current])}
          onValueChange={(next: ValueKind | null) => {
            setKind(next ?? "string");
          }}
        >
          {valueKinds.map((option) => (
            <Select.Option key={option} value={option}>
              {kindLabels[option]}
            </Select.Option>
          ))}
        </Select>
        {kind === "boolean" ? (
          <Select
            label="Value"
            value={text === "false" ? "false" : "true"}
            renderValue={(current) => (current === "false" ? "false" : "true")}
            onValueChange={(next) => {
              setText(next ?? "true");
            }}
          >
            <Select.Option value="true">true</Select.Option>
            <Select.Option value="false">false</Select.Option>
          </Select>
        ) : (
          <Input
            label="Value"
            value={text}
            spellCheck={false}
            inputMode={kind === "number" ? "decimal" : undefined}
            onChange={(event) => {
              setText(event.target.value);
            }}
          />
        )}
      </div>
      <Input
        label="Expires"
        type="datetime-local"
        value={expiry}
        onChange={(event) => {
          setExpiry(event.target.value);
        }}
      />
      <Input
        label="Comment"
        value={comment}
        placeholder="Optional"
        onChange={(event) => {
          setComment(event.target.value);
        }}
      />
      <DialogError message={set.isError ? errorMessage(set.error) : undefined} />
      <FormFooter
        label={attribute === null ? "Add" : "Save"}
        pending={set.isPending}
        disabled={!valid}
      />
    </form>
  );
}

/** The typed value the form sends, or undefined when the text does not parse as the kind. */
export function parseValue(kind: ValueKind, text: string): string | number | boolean | undefined {
  if (kind === "boolean") {
    return text !== "false";
  }

  const trimmed = text.trim();

  if (kind === "number") {
    const number = Number(trimmed);

    return trimmed === "" || Number.isNaN(number) ? undefined : number;
  }

  return trimmed === "" ? undefined : trimmed;
}
