import { ClockIcon, GlobeIcon, LightningIcon, PathIcon, StackIcon } from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import type { ReactElement } from "react";

import { routeFilterLabels, routeFilters } from "~/components/networks/routes-model.ts";
import type { RouteFilter, RouteFilterState } from "~/components/networks/routes-model.ts";
import { FacetChip } from "~/components/ui/facet-chips.tsx";

const icons: Record<RouteFilter, Icon> = {
  pending: ClockIcon,
  exit: GlobeIcon,
  learned: LightningIcon,
  networks: StackIcon,
};

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
      <FacetChip pressed={none} onClick={onClear} icon={PathIcon}>
        {total === 1 ? "1 route" : `${total} routes`}
      </FacetChip>
      <span aria-hidden className="h-5 border-l border-kumo-line" />
      {routeFilters.map((filter) => (
        <FacetChip
          key={filter}
          pressed={state[filter]}
          icon={icons[filter]}
          count={counts[filter]}
          onClick={() => {
            onToggle(filter);
          }}
        >
          {routeFilterLabels[filter]}
        </FacetChip>
      ))}
    </div>
  );
}
