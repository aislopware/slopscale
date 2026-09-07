import { Button } from "@cloudflare/kumo/components/button";
import { TrashIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { SectionRow } from "~/components/ui/section.tsx";

const iconSize = 16;

export interface Entry {
  readonly key: string;
  readonly value: ReactNode;
  /** Shown to the right of the value, before the actions. */
  readonly aside?: ReactNode;
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
  readonly empty: string;
  readonly canEdit: boolean;
  readonly pending: boolean;
}): ReactElement {
  if (entries.length === 0) {
    return (
      <SectionRow>
        <p className="text-kumo-subtle">{empty}</p>
      </SectionRow>
    );
  }

  return (
    <>
      {entries.map((entry) => (
        <SectionRow key={entry.key} className="flex items-center justify-between gap-4 py-2.5">
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <span className="min-w-0 font-mono text-sm break-all">{entry.value}</span>
            {entry.aside}
          </div>
          {canEdit && entry.onRemove !== undefined ? (
            <RemoveButton label={entry.removeLabel ?? "Remove"} disabled={pending} entry={entry} />
          ) : null}
        </SectionRow>
      ))}
    </>
  );
}

function RemoveButton({
  label,
  disabled,
  entry,
}: {
  readonly label: string;
  readonly disabled: boolean;
  readonly entry: Entry;
}): ReactElement {
  return (
    <Button
      variant="ghost"
      shape="square"
      size="sm"
      icon={<TrashIcon size={iconSize} />}
      aria-label={label}
      disabled={disabled}
      onClick={() => {
        entry.onRemove?.();
      }}
    />
  );
}
