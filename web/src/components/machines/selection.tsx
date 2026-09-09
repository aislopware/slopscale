import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { createContext, use, useState } from "react";
import type { ReactElement, ReactNode } from "react";

/** What the machines table's checkbox column reads and writes. */
export interface MachineSelection {
  /** The chosen machines that the current filters and search still show. */
  readonly selected: ReadonlySet<string>;
  /** Every machine the filters and the search leave, which is what the header checkbox covers. */
  readonly ids: readonly string[];
  readonly toggle: (id: string) => void;
  readonly toggleAll: () => void;
  readonly clear: () => void;
}

const SelectionContext = createContext<MachineSelection | null>(null);

/**
 * The ticked machines the current view still shows. A machine a filter or the search hides is not
 * selected any more, so narrowing the list narrows the action instead of acting on rows nobody can
 * see, and widening it again does not bring back a tick the operator has forgotten about.
 */
export function visibleSelection(ids: readonly string[], chosen: ReadonlySet<string>): Set<string> {
  return new Set(ids.filter((id) => chosen.has(id)));
}

/**
 * The machines chosen for a bulk action. The state lives on the page, and the checkbox column reads
 * it from here rather than through the table's meta, so the column stays a module-level constant
 * and the table is not rebuilt on every click.
 *
 * `ids` is every row the table would show without paging — the filters and the search applied, the
 * page not — so "select all" means what the operator can see, not what the first page holds.
 */
export function useMachineSelection(ids: readonly string[]): MachineSelection {
  const [chosen, setChosen] = useState<ReadonlySet<string>>(() => new Set<string>());
  const selected = visibleSelection(ids, chosen);
  const allChosen = ids.length > 0 && selected.size === ids.length;

  return {
    selected,
    ids,
    toggle: (id) => {
      setChosen((previous) => {
        const next = new Set(previous);

        if (next.has(id)) {
          next.delete(id);
        } else {
          next.add(id);
        }

        return next;
      });
    },
    toggleAll: () => {
      setChosen(allChosen ? new Set<string>() : new Set(ids));
    },
    clear: () => {
      setChosen(new Set<string>());
    },
  };
}

export function SelectionProvider({
  selection,
  children,
}: {
  readonly selection: MachineSelection;
  readonly children: ReactNode;
}): ReactElement {
  return <SelectionContext value={selection}>{children}</SelectionContext>;
}

/** Ticks every machine the filters leave, not only the ones on this page of the table. */
export function SelectAllCheckbox(): ReactNode {
  const selection = use(SelectionContext);

  if (selection === null) {
    return null;
  }

  const { selected, ids } = selection;

  return (
    <Checkbox
      aria-label="Select every machine in this list"
      checked={ids.length > 0 && selected.size === ids.length}
      indeterminate={selected.size > 0 && selected.size < ids.length}
      onCheckedChange={() => {
        selection.toggleAll();
      }}
    />
  );
}

export function SelectRowCheckbox({
  id,
  name,
}: {
  readonly id: string;
  readonly name: string;
}): ReactNode {
  const selection = use(SelectionContext);

  if (selection === null) {
    return null;
  }

  return (
    <Checkbox
      aria-label={`Select ${name}`}
      checked={selection.selected.has(id)}
      onCheckedChange={() => {
        selection.toggle(id);
      }}
    />
  );
}
