import { Badge } from "@cloudflare/kumo/components/badge";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { api } from "~/api/client.ts";
import type { Node } from "~/api/queries.ts";
import { SectionRow } from "~/components/ui/section.tsx";

/** The postures the machine satisfies right now, so an operator can see why a rule admits it. */
export function MatchedPostures({ node }: { readonly node: Node }): ReactElement | null {
  const matched = useQuery(
    api.queryOptions("get", "/api/v1/node/{nodeId}/postures", {
      params: { path: { nodeId: node.id } },
    }),
  );
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
        <p className="text-sm text-kumo-subtle">
          This machine satisfies no posture, so rules that require one do not admit it.
        </p>
      ) : (
        <div className="flex flex-wrap gap-1">
          {postures.map((posture) => (
            <Badge key={posture.id} variant="secondary" className="max-w-64">
              <span className="truncate">{posture.name}</span>
            </Badge>
          ))}
        </div>
      )}
    </SectionRow>
  );
}
