import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import { FormFooter } from "~/components/machines/dialogs.tsx";
import { DialogContent, DialogError, DialogRoot } from "~/components/ui/dialog.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface ValueDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  readonly title: string;
  readonly description: string;
  readonly label: string;
  readonly placeholder: string;
  /** What is wrong with the trimmed value, or null when it can be saved. */
  readonly validate: (value: string) => string | null;
  /** Normalizes the value before it is checked and sent. */
  readonly normalize?: (value: string) => string;
  /** Sends the value; resolves through the mutation the dialog watches. */
  readonly onSubmit: (value: string, done: () => void) => void;
  readonly successMessage: string;
  /** The request the dialog watches for its pending state and error. */
  readonly mutation: PendingMutation;
}

/** What a dialog needs to know about the request it sends. */
export interface PendingMutation {
  readonly isPending: boolean;
  readonly isError: boolean;
  readonly error: unknown;
}

const trim = (value: string): string => value.trim();

/**
 * A one-field dialog: nameserver, search domain, map URL. The form mounts with the dialog so it
 * starts empty.
 */
export function ValueDialog(props: ValueDialogProps): ReactElement {
  return (
    <DialogRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent size="base" title={props.title} description={props.description}>
        <ValueForm {...props} />
      </DialogContent>
    </DialogRoot>
  );
}

function ValueForm({
  label,
  placeholder,
  validate,
  normalize = trim,
  onSubmit,
  successMessage,
  mutation,
  onOpenChange,
}: Omit<ValueDialogProps, "open" | "title" | "description">): ReactElement {
  const [value, setValue] = useState("");
  const [touched, setTouched] = useState(false);
  const clean = normalize(value);
  const issue = validate(clean);

  function submit(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    onSubmit(clean, () => {
      toast.success(successMessage);
      onOpenChange(false);
    });
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <Input
        label={label}
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        autoComplete="off"
        onChange={(event) => {
          setValue(event.target.value);
        }}
        onBlur={() => {
          setTouched(true);
        }}
        {...(touched && issue !== null ? { error: issue } : {})}
      />
      <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
      <FormFooter label="Add" pending={mutation.isPending} disabled={clean === ""} />
    </form>
  );
}
