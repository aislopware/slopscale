import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { TrashIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import type { ReactElement, ReactNode, SubmitEvent } from "react";

import { errorMessage } from "~/api/error.ts";
import type { PendingMutation } from "~/components/dns/value-dialog.tsx";
import { DialogError } from "~/components/ui/dialog.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { Note } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";

export interface StagedAddPanelProps {
  readonly title: string;
  readonly description: ReactNode;
  readonly label: string;
  readonly placeholder: string;
  /** What is wrong with the normalized value, or null when it can be added. */
  readonly validate: (value: string) => string | null;
  /** Normalizes the value before it is checked and staged. */
  readonly normalize?: (value: string) => string;
  /** The values the list already holds, so one is not staged twice. */
  readonly existing: readonly string[];
  /** Sends every staged value at once; `done` closes the panel. */
  readonly onSubmit: (values: readonly string[], done: () => void) => void;
  /** The toast once they are saved, given how many there were. */
  readonly successMessage: (count: number) => string;
  readonly mutation: PendingMutation;
  readonly onClose: () => void;
}

const trim = (value: string): string => value.trim();

function countEntries(count: number): string {
  return count === 1 ? "1 entry" : `${count} entries`;
}

/**
 * Zero Trust's split tunnel add panel, for the one-value lists DNS and the relay map keep: a card
 * that opens above the list with a field and "Add to list", the staged entries under it with a
 * trash button each, an "Unsaved changes" badge while there are any, and "Cancel" and "Save N
 * entries" at the foot. The entries are checked as they are staged and sent in one request, so
 * adding three resolvers is one change for the machines rather than three.
 */
export function StagedAddPanel({
  title,
  description,
  label,
  placeholder,
  validate,
  normalize = trim,
  existing,
  onSubmit,
  successMessage,
  mutation,
  onClose,
}: StagedAddPanelProps): ReactElement {
  const [value, setValue] = useState("");
  const [staged, setStaged] = useState<readonly string[]>([]);
  const [touched, setTouched] = useState(false);
  const { reset } = mutation;

  // A failure from an earlier save must not greet a panel opened afresh.
  useEffect(() => {
    reset?.();
  }, [reset]);

  const clean = normalize(value);
  const issue =
    existing.includes(clean) || staged.includes(clean)
      ? "That one is already on the list."
      : validate(clean);

  function stage(event: SubmitEvent<HTMLFormElement>): void {
    event.preventDefault();
    setTouched(true);

    if (issue !== null) {
      return;
    }

    setStaged([...staged, clean]);
    setValue("");
    setTouched(false);
  }

  return (
    <Section
      title={title}
      meta={staged.length === 0 ? undefined : <Badge variant="warning">Unsaved changes</Badge>}
      description={description}
    >
      <form onSubmit={stage} className="flex flex-col gap-3 p-4">
        <div className="grid items-start gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
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
              setTouched(clean !== "");
            }}
            {...(touched && issue !== null ? { error: issue } : {})}
          />
          {/* The field's label takes a line above the input; the button sits level with the input. */}
          <Button type="submit" variant="secondary" disabled={clean === ""} className="sm:mt-6">
            Add to list
          </Button>
        </div>
      </form>
      {staged.map((entry) => (
        <SectionRow key={entry} className="flex items-center justify-between gap-4 py-2">
          <span className="min-w-0 font-mono text-sm break-all">{entry}</span>
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            icon={TrashIcon}
            aria-label={`Remove ${entry} from the list`}
            onClick={() => {
              setStaged(staged.filter((candidate) => candidate !== entry));
            }}
          />
        </SectionRow>
      ))}
      <div className="flex flex-col gap-3 border-t border-kumo-line px-4 py-3">
        <DialogError message={mutation.isError ? errorMessage(mutation.error) : undefined} />
        <div className="flex flex-wrap items-center justify-between gap-3">
          {staged.length === 0 ? (
            <span />
          ) : (
            <Note tone="warning">
              Click Save to apply the entries in this list to every machine.
            </Note>
          )}
          <div className="ms-auto flex items-center gap-2">
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="primary"
              disabled={staged.length === 0}
              loading={mutation.isPending}
              onClick={() => {
                onSubmit(staged, () => {
                  toast.success(successMessage(staged.length));
                  onClose();
                });
              }}
            >
              {`Save ${countEntries(staged.length)}`}
            </Button>
          </div>
        </div>
      </div>
    </Section>
  );
}
