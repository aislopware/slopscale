import { Button } from "@cloudflare/kumo/components/button";
import { TrashIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { SectionEmpty, SectionRow } from "~/components/ui/section.tsx";
import type { SectionEmptyProps } from "~/components/ui/section.tsx";

const iconSize = 16;

export interface Entry {
  readonly key: string;
  readonly value: ReactNode;
  /** Shown next to the value, such as a badge saying where it came from. */
  readonly aside?: ReactNode;
  /** A control for the entry, aligned with the actions column so rows line up. */
  readonly control?: ReactNode;
  /** Absent when the entry cannot be removed, such as the base domain. */
  readonly onRemove?: (() => void) | undefined;
  readonly removeLabel?: string;
}

/** Rows of values with a remove button each, and a quiet line when there are none. */
export function EntryList({
  entries,
  empty,
  canEdit,
  pending,
}: {
  readonly entries: readonly Entry[];
  /** What the panel says when there is nothing in it. */
  readonly empty: SectionEmptyProps;
  readonly canEdit: boolean;
  readonly pending: boolean;
}): ReactElement {
  if (entries.length === 0) {
    return <SectionEmpty {...empty} />;
  }

  return (
    <>
      {entries.map((entry) => (
        <SectionRow key={entry.key} className="flex items-center justify-between gap-4 py-2.5">
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <span className="min-w-0 font-mono text-sm break-all">{entry.value}</span>
            {entry.aside}
          </div>
          {entry.control !== undefined || (canEdit && entry.onRemove !== undefined) ? (
            <div className="flex shrink-0 items-center gap-3">
              {entry.control}
              {canEdit && entry.onRemove !== undefined ? (
                <RemoveButton
                  label={entry.removeLabel ?? "Remove"}
                  disabled={pending}
                  onRemove={() => {
                    entry.onRemove?.();
                  }}
                />
              ) : null}
            </div>
          ) : null}
        </SectionRow>
      ))}
    </>
  );
}

function RemoveButton({
  label,
  disabled,
  onRemove,
}: {
  readonly label: string;
  readonly disabled: boolean;
  readonly onRemove: () => void;
}): ReactElement {
  return (
    <Button
      variant="ghost"
      shape="square"
      size="sm"
      icon={<TrashIcon size={iconSize} />}
      aria-label={label}
      disabled={disabled}
      onClick={onRemove}
    />
  );
}

/**
 * The room a row's remove button takes, for a row that has none but whose control must end on the
 * same edge as the rows that do. It renders the button itself, hidden, so the two cannot drift
 * apart when the button changes.
 */
export function EntryActionSpacer(): ReactElement {
  return (
    <span aria-hidden className="invisible">
      <RemoveButton label="" disabled onRemove={noop} />
    </span>
  );
}

function noop(): void {
  // The spacer is not interactive.
}
