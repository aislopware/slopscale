import { cn } from "@cloudflare/kumo/utils";
import { ClockIcon, GlobeIcon, LightningIcon, PathIcon, StackIcon } from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { routeFilterLabels, routeFilters } from "~/components/networks/routes-model.ts";
import type { RouteFilter, RouteFilterState } from "~/components/networks/routes-model.ts";

const icons: Record<RouteFilter, Icon> = {
  pending: ClockIcon,
  exit: GlobeIcon,
  learned: LightningIcon,
  networks: StackIcon,
};

const iconSize = 14;

/**
 * The facet chips over the dashboard's Routes table ("6 routes", "System 6", "CIDR 6"): a pill per
 * kind of route with an icon and its count, the picked ones filled. The first chip is every route
 * and is filled while nothing narrows the list; pressing it clears the others. The kinds can be on
 * together, which is why they are toggles rather than segments: pending narrows whatever else is
 * picked, and the three kinds widen each other.
 */
export function RouteFilterChips({
  state,
  counts,
  total,
  onToggle,
  onClear,
}: {
  readonly state: RouteFilterState;
  readonly counts: Record<RouteFilter, number>;
  /** Every advertised prefix, for the first chip. */
  readonly total: number;
  readonly onToggle: (filter: RouteFilter) => void;
  readonly onClear: () => void;
}): ReactElement {
  const none = routeFilters.every((filter) => !state[filter]);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Chip pressed={none} onClick={onClear} icon={PathIcon}>
        {total === 1 ? "1 route" : `${total} routes`}
      </Chip>
      <span aria-hidden className="h-5 border-l border-kumo-line" />
      {routeFilters.map((filter) => (
        <Chip
          key={filter}
          pressed={state[filter]}
          icon={icons[filter]}
          count={counts[filter]}
          onClick={() => {
            onToggle(filter);
          }}
        >
          {routeFilterLabels[filter]}
        </Chip>
      ))}
    </div>
  );
}

function Chip({
  pressed,
  icon: ChipIcon,
  count,
  onClick,
  children,
}: {
  readonly pressed: boolean;
  readonly icon: Icon;
  readonly count?: number;
  readonly onClick: () => void;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-full px-3 text-sm text-kumo-default ring outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus",
        pressed
          ? "bg-kumo-fill font-medium ring-kumo-interact"
          : "bg-kumo-base ring-kumo-hairline hover:ring-kumo-interact",
      )}
    >
      <ChipIcon size={iconSize} aria-hidden className="text-kumo-subtle" />
      {children}
      {count === undefined ? null : (
        <span className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full px-1 text-xs text-kumo-subtle tabular-nums ring ring-kumo-line">
          {count}
        </span>
      )}
    </button>
  );
}
