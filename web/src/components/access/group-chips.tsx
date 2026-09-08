import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { Group } from "~/api/queries.ts";
import { groupName, isBuiltin } from "~/components/access/model.ts";

/** One badge per group id; the builtin group is drawn hollow so it reads as "everyone". */
export function GroupChips({
  ids,
  groups,
  emptyLabel = "—",
}: {
  readonly ids: readonly string[];
  readonly groups: readonly Group[];
  readonly emptyLabel?: string;
}): ReactElement {
  if (ids.length === 0) {
    return <span className="text-kumo-subtle">{emptyLabel}</span>;
  }

  return (
    <div className="flex flex-wrap items-center gap-1">
      {ids.map((id) => {
        const group = groups.find((candidate) => candidate.id === id);

        return (
          <Badge
            key={id}
            variant={group !== undefined && isBuiltin(group) ? "outline" : "secondary"}
            className="max-w-48"
          >
            <span className="truncate">{groupName(groups, id)}</span>
          </Badge>
        );
      })}
    </div>
  );
}
