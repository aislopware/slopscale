import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";

import { targetKinds } from "~/components/audit/cells.tsx";
import type { LogFilter } from "~/components/table/log-filters.tsx";
import { useDebounced } from "~/lib/use-debounced.ts";

/** How long typing has to pause before the typed text reaches the address. */
export const typingSettleMs = 300;

/** The target part of the audit log's address. */
export interface TargetSearch {
  readonly targetKind: string;
  readonly targetId: string;
}

export interface TargetFilter {
  /** The Filters popover's Target row. */
  readonly filter: LogFilter;
  /** Empties the typed id, for a caller that clears the address itself. */
  readonly clearDraft: () => void;
}

/** A typed box's own text, reset whenever the address changes it from outside. */
export function useDraft(fromSearch: string): [string, (next: string) => void] {
  const [draft, setDraft] = useState(fromSearch);
  const [seen, setSeen] = useState(fromSearch);

  if (seen !== fromSearch) {
    setSeen(fromSearch);
    setDraft(fromSearch);
  }

  return [draft, setDraft];
}

/**
 * The kinds of object an event can be about, by their console names, for the Target filter. Two
 * kinds the API spells two ways share a name, so those carry the API's spelling too.
 */
const targetKindOptions = [
  { value: "", label: "Any kind" },
  ...Object.entries(targetKinds)
    .map(([kind, label]) => ({
      value: kind,
      label:
        Object.values(targetKinds).filter((other) => other === label).length > 1
          ? `${label} (${kind})`
          : label,
    }))
    .toSorted((left, right) => left.label.localeCompare(right.label)),
];

/**
 * The Target row of the Filters popover: the kind of object, picked, and its id, typed. The typed
 * id reaches the address, and so the server, once it stops changing.
 */
export function useTargetFilter({
  targetKind,
  targetId,
  onChange,
}: {
  readonly targetKind: string;
  readonly targetId: string;
  /** Moves the address to the target given; `replace` while the id is typed. */
  readonly onChange: (target: TargetSearch, replace: boolean) => void;
}): TargetFilter {
  const [idDraft, setIdDraft] = useDraft(targetId);
  const settledId = useDebounced(idDraft.trim(), typingSettleMs);

  useEffect(() => {
    if (settledId === idDraft.trim() && settledId !== targetId) {
      onChange({ targetKind, targetId: settledId }, true);
    }
  }, [settledId, idDraft, targetKind, targetId, onChange]);

  const filter: LogFilter = {
    id: "target",
    label: "Target",
    operator: "is",
    value: (
      // The id takes a line of its own under the kind on a phone.
      <div className="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_6rem]">
        <Select
          aria-label="Target kind"
          className="w-full min-w-0"
          value={targetKind}
          items={targetKindOptions}
          onValueChange={(value) => {
            onChange({ targetKind: value ?? "", targetId }, false);
          }}
        />
        <Input
          aria-label="Target ID"
          className="w-full min-w-0"
          placeholder="Any ID"
          value={idDraft}
          onChange={(event) => {
            setIdDraft(event.target.value);
          }}
        />
      </div>
    ),
    active: targetKind !== "" || targetId !== "",
    addable: true,
    onRemove: () => {
      setIdDraft("");
      onChange({ targetKind: "", targetId: "" }, false);
    },
  };

  return {
    filter,
    clearDraft: () => {
      setIdDraft("");
    },
  };
}
