import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";

import { nodeAppConnectorRoutesQuery } from "~/api/queries.ts";
import type { App } from "~/api/queries.ts";
import { can } from "~/auth/me.ts";
import type { Me } from "~/auth/me.ts";
import { learnedForApp } from "~/components/apps/model.ts";

/** What the connected connectors of one app have learned for it. */
export interface LearnedCount {
  /** Distinct addresses learned for the app's domains, from the connectors that have answered. */
  readonly count: number;
  /** How many of the app's connected connectors have answered so far. */
  readonly answered: number;
  /** How many of the app's connectors are connected and so can be asked. */
  readonly connected: number;
}

/** The learned routes of every app, by app id; null when the caller may not ask machines. */
export type LearnedCounts = ReadonlyMap<string, LearnedCount> | null;

type Answer = Readonly<Record<string, string[] | null>>;

/** Only the answers, so a refetch that changes nothing changes nothing here either. */
function answersOf(
  results: readonly { readonly data?: { readonly domains: Answer } | undefined }[],
): (Answer | undefined)[] {
  return results.map((result) => result.data?.domains);
}

/** The connectors an app can be asked through: connected, and running the connector service. */
function askable(app: App): readonly string[] {
  return app.nodes.filter((node) => node.online && node.connector).map((node) => node.nodeId);
}

/**
 * Asks every connected connector what it has learned and narrows each answer to the app: the server
 * only knows how many single-address routes a machine advertises, and a machine serving three apps
 * would report the same figure for all three. The question is the one the machine page asks, so
 * both agree, and a connector shared by several apps is asked once.
 */
export function useLearnedCounts(apps: readonly App[], me: Me): LearnedCounts {
  const allowed = can(me, "devices:core:read");
  const nodeIds = useMemo(
    () => (allowed ? [...new Set(apps.flatMap((app) => askable(app)))].toSorted() : []),
    [apps, allowed],
  );
  const answers = useQueries({
    queries: nodeIds.map((nodeId) => nodeAppConnectorRoutesQuery(nodeId)),
    combine: answersOf,
  });

  return useMemo(() => {
    if (!allowed) {
      return null;
    }

    const byNode = new Map<string, Answer | undefined>();

    nodeIds.forEach((nodeId, index) => {
      byNode.set(nodeId, answers[index]);
    });

    return new Map(
      apps.map((app) => {
        const connected = askable(app);
        const heard = connected
          .map((nodeId) => byNode.get(nodeId))
          .filter((answer) => answer !== undefined);

        return [
          app.id,
          {
            count: learnedForApp(app.domains, heard),
            answered: heard.length,
            connected: connected.length,
          },
        ];
      }),
    );
  }, [allowed, apps, nodeIds, answers]);
}
