import { Button } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement } from "react";

import { routeFilterLabels, routeFilters } from "~/components/networks/routes-model.ts";
import type { RouteFilter, RouteFilterState } from "~/components/networks/routes-model.ts";

/**
 * The kinds of route worth pulling out of a long list, each carrying how many prefixes it would
 * keep. Several can be on at once, which is why they are toggles rather than segments: pending
 * narrows whatever else is picked, and the three kinds widen each other.
 */
export function RouteFilterChips({
  state,
  counts,
  onToggle,
}: {
  readonly state: RouteFilterState;
  readonly counts: Record<RouteFilter, number>;
  readonly onToggle: (filter: RouteFilter) => void;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {routeFilters.map((filter) => (
        <Button
          key={filter}
          variant={state[filter] ? "secondary" : "ghost"}
          size="sm"
          aria-pressed={state[filter]}
          className={cn(state[filter] && "bg-kumo-tint")}
          onClick={() => {
            onToggle(filter);
          }}
        >
          {routeFilterLabels[filter]}
          <span
            className={cn("tabular-nums", state[filter] ? "text-kumo-default" : "text-kumo-subtle")}
          >
            {counts[filter]}
          </span>
        </Button>
      ))}
    </div>
  );
}
