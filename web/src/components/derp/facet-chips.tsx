import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

export interface Facet<TValue extends string> {
  readonly value: TValue;
  readonly label: string;
  readonly count: number;
}

/**
 * The dashboard's facet chips over a list (Networking > Routes: "6 routes · System 6 · CIDR 6"):
 * rounded-full pills on a hairline ring, each with its count, the picked one filled. They narrow
 * the rows below to one kind; the first facet is usually every row. Each is a toggle button that
 * says whether it is the one picked.
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
      {facets.map((facet) => {
        const selected = facet.value === value;

        return (
          <button
            key={facet.value}
            type="button"
            aria-pressed={selected}
            onClick={() => {
              onChange(facet.value);
            }}
            className={cn(
              "inline-flex h-7 items-center gap-2 rounded-full px-3 text-sm text-kumo-default ring outline-none",
              "transition-[box-shadow,background-color] focus-visible:ring-2 focus-visible:ring-kumo-focus",
              selected
                ? "bg-kumo-fill font-medium ring-kumo-line"
                : "bg-kumo-base ring-kumo-hairline hover:ring-kumo-line",
            )}
          >
            {facet.label}
            <span
              className={cn(
                "inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-xs tabular-nums",
                selected ? "bg-kumo-base text-kumo-default" : "bg-kumo-fill text-kumo-subtle",
              )}
            >
              {facet.count}
            </span>
          </button>
        );
      })}
    </fieldset>
  );
}
