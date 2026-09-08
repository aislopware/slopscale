import { Button } from "@cloudflare/kumo/components/button";
import { XIcon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import type { User } from "~/api/queries.ts";
import { defaultStatus, statusFilterLabels } from "~/components/machines/filters.ts";
import type { MachineFilterState } from "~/components/machines/filters.ts";
import { FrameBand } from "~/components/ui/frame.tsx";
import { userLabel } from "~/lib/node.ts";

const removeIconSize = 12;

/** One narrowing in force: what it is called, what it is set to, and how to drop it. */
export interface FilterChip {
  /** The kind of filter, as a word: "Owner", "Tag", "Search". */
  readonly name: string;
  readonly value: string;
  readonly onRemove: () => void;
}

/**
 * The filters in force, under the toolbar. A control shows its own value, but a filter that came
 * from a link or from another page is invisible until it is spelled out, and each chip is the way
 * back out of it.
 */
export function FilterChips({
  chips,
  onClearAll,
}: {
  readonly chips: readonly FilterChip[];
  readonly onClearAll: () => void;
}): ReactElement | null {
  if (chips.length === 0) {
    return null;
  }

  return (
    // px-5 lines the first chip up with the first column of the table below.
    <FrameBand className="flex flex-wrap items-center gap-1.5 px-5">
      <span className="mr-0.5 text-xs text-kumo-subtle">Filtered by</span>
      {chips.map((chip) => (
        <Button
          key={`${chip.name}:${chip.value}`}
          variant="secondary"
          size="xs"
          aria-label={`Remove the ${chip.name.toLowerCase()} filter`}
          onClick={() => {
            chip.onRemove();
          }}
        >
          <span className="text-kumo-subtle">{chip.name}</span>
          <span className="max-w-40 truncate">{chip.value}</span>
          <XIcon size={removeIconSize} aria-hidden />
        </Button>
      ))}
      {chips.length > 1 ? (
        <Button variant="ghost" size="xs" onClick={onClearAll}>
          Clear all
        </Button>
      ) : null}
    </FrameBand>
  );
}

/**
 * A chip for each narrowing in force, in the order the toolbar's controls sit. Each one hands back
 * the whole state with its own field back at the default, so the page has one way to navigate.
 */
export function machineChips(
  state: MachineFilterState,
  users: readonly User[] | undefined,
  onChange: (next: MachineFilterState) => void,
): FilterChip[] {
  const chips: FilterChip[] = [];

  if (state.query !== "") {
    chips.push({
      name: "Search",
      value: state.query,
      onRemove: () => {
        onChange({ ...state, query: "" });
      },
    });
  }

  if (state.status !== defaultStatus) {
    chips.push({
      name: "Status",
      value: statusFilterLabels[state.status],
      onRemove: () => {
        onChange({ ...state, status: defaultStatus });
      },
    });
  }

  if (state.user !== "") {
    const owner = users?.find((candidate) => candidate.id === state.user);

    chips.push({
      name: "Owner",
      value: owner === undefined ? `#${state.user}` : userLabel(owner),
      onRemove: () => {
        onChange({ ...state, user: "" });
      },
    });
  }

  if (state.tag !== "") {
    chips.push({
      name: "Tag",
      value: state.tag,
      onRemove: () => {
        onChange({ ...state, tag: "" });
      },
    });
  }

  return chips;
}
