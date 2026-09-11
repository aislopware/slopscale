import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { Node } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { SectionRow } from "~/components/ui/section.tsx";
import { ValueList } from "~/components/ui/value-list.tsx";

/** The postures the machine satisfies right now, so an operator can see why a rule admits it. */
export function MatchedPostures({
  node,
  me,
}: {
  readonly node: Node;
  readonly me: Me;
}): ReactElement | null {
  const readable = can(me, "devices:posture_attributes:read");
  const matched = useQuery({
    ...api.queryOptions("get", "/api/v1/node/{nodeId}/postures", {
      params: { path: { nodeId: node.id } },
    }),
    enabled: readable,
  });

  if (!readable) {
    return null;
  }

  if (matched.isError) {
    return (
      <SectionRow className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between gap-4">
          <span className="text-sm font-medium text-kumo-default">Postures</span>
          <Link to="/policy/postures" className="text-xs text-kumo-link">
            Manage postures
          </Link>
        </div>
        <p className="text-xs text-kumo-subtle">Could not load postures.</p>
      </SectionRow>
    );
  }

  const postures = matched.data?.postures;

  if (postures === undefined) {
    return null;
  }

  return (
    <SectionRow className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-4">
        <span className="text-sm font-medium text-kumo-default">Postures</span>
        <Link to="/policy/postures" className="text-xs text-kumo-link">
          Manage postures
        </Link>
      </div>
      {postures.length === 0 ? (
        <p className="text-sm text-kumo-subtle">No posture matches this machine.</p>
      ) : (
        <ValueList items={postures.map((posture) => posture.name)} />
      )}
    </SectionRow>
  );
}
