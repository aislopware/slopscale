import type { ReactElement } from "react";

import type { Group } from "~/api/queries.ts";
import { groupName } from "~/components/access/model.ts";
import { ValueList } from "~/components/ui/value-list.tsx";

/** The groups behind a list of ids, by name, one per line. */
export function GroupNames({
  ids,
  groups,
  emptyLabel = "—",
  max,
}: {
  readonly ids: readonly string[];
  readonly groups: readonly Group[];
  readonly emptyLabel?: string;
  /** How many names a table cell shows before the rest are counted. */
  readonly max?: number;
}): ReactElement {
  return (
    <ValueList
      items={ids.map((id) => groupName(groups, id))}
      empty={emptyLabel}
      {...(max === undefined ? {} : { max })}
    />
  );
}
