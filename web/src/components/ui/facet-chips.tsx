import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

export interface Facet<TValue extends string> {
  readonly value: TValue;
  readonly label: string;
  readonly count: number;
  readonly icon?: Icon;
}

const iconSize = 14;

/**
 * One of the dashboard's facet chips over a list (Networking > Routes: "6 routes · System 6 · CIDR
 * 6"): a rounded-full pill on a hairline ring with an optional icon and its count, filled while it
 * is pressed. The spec's `ring-kumo-ring` is not a Kumo token; `kumo-interact`, the solid control
 * edge, is the ring it stands for. A toggle button that says whether it is pressed, for a list
 * whose kinds can be on together; `FacetChips` is the one-of-many group.
 */
export function FacetChip({
  pressed,
  icon: ChipIcon,
  count,
  onClick,
  children,
}: {
  readonly pressed: boolean;
  readonly icon?: Icon | undefined;
  /** The rows the chip leaves, in a small pill after the label. */
  readonly count?: number | undefined;
  readonly onClick: () => void;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-full px-3 text-sm text-kumo-default ring outline-none",
        "transition-[box-shadow,background-color] focus-visible:ring-2 focus-visible:ring-kumo-focus",
        pressed
          ? "bg-kumo-fill font-medium ring-kumo-interact"
          : "bg-kumo-base ring-kumo-hairline hover:ring-kumo-interact",
      )}
    >
      {ChipIcon === undefined ? null : (
        <ChipIcon size={iconSize} aria-hidden className="text-kumo-subtle" />
      )}
      {children}
      {count === undefined ? null : (
        <span
          className={cn(
            "ml-0.5 inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-xs tabular-nums",
            pressed ? "bg-kumo-base text-kumo-default" : "bg-kumo-fill text-kumo-subtle",
          )}
        >
          {count}
        </span>
      )}
    </button>
  );
}

/**
 * A row of facet chips of which one is picked, narrowing the rows below to one kind; the first
 * facet is usually every row.
 */
export function FacetChips<TValue extends string>({
  label,
  facets,
  value,
  onChange,
}: {
  /** The accessible name of the group: "Regions by source". */
  readonly label: string;
  readonly facets: readonly Facet<TValue>[];
  readonly value: TValue;
  readonly onChange: (value: TValue) => void;
}): ReactElement {
  return (
    <fieldset className="flex flex-wrap items-center gap-2">
      <legend className="sr-only">{label}</legend>
      {facets.map((facet) => (
        <FacetChip
          key={facet.value}
          pressed={facet.value === value}
          icon={facet.icon}
          count={facet.count}
          onClick={() => {
            onChange(facet.value);
          }}
        >
          {facet.label}
        </FacetChip>
      ))}
    </fieldset>
  );
}
