import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { fetchClient } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import { plural } from "~/components/overview/plural.ts";
import { toast } from "~/components/ui/toast.ts";

/** What the machines table can do to a whole selection at once. */
export type BulkAction = "approve" | "expire" | "delete";

interface ActionWords {
  /** "approve 3 machines", for the sentence a failure is reported in. */
  readonly verb: string;
  /** "3 machines approved", for the sentence a success is reported in. */
  readonly past: string;
}

const words: Record<BulkAction, ActionWords> = {
  approve: { verb: "approve", past: "approved" },
  expire: { verb: "expire the key of", past: "keys expired" },
  delete: { verb: "delete", past: "deleted" },
};

/** The API has no batch endpoint, so one machine is one request. */
async function callOne(action: BulkAction, nodeId: string): Promise<void> {
  const params = { path: { nodeId } };

  if (action === "delete") {
    await fetchClient.DELETE("/api/v1/node/{nodeId}", { params });

    return;
  }

  if (action === "approve") {
    await fetchClient.POST("/api/v1/node/{nodeId}/approve", { params, body: {} });

    return;
  }

  await fetchClient.POST("/api/v1/node/{nodeId}/expire", { params, body: {} });
}

export interface BulkRunner {
  /** The action in flight, so its button can spin and the rest can be held. */
  readonly running: BulkAction | null;
  readonly run: (action: BulkAction, ids: readonly string[]) => Promise<void>;
}

/**
 * Runs one action over a selection, one request at a time so the server is not asked to do
 * everything at once, and reports the whole run in a single toast. The machine list is refreshed
 * once at the end rather than after each call.
 *
 * The per-machine mutations are not reused here: each of them raises its own toast and the delete
 * one navigates away, which is right for a single row and wrong for forty.
 */
export function useMachineBulk(): BulkRunner {
  const queryClient = useQueryClient();
  const [running, setRunning] = useState<BulkAction | null>(null);

  return {
    running,
    run: async (action, ids) => {
      setRunning(action);

      const failures = await runInTurn(action, ids);

      await (action === "delete"
        ? invalidate(
            queryClient,
            "/api/v1/node",
            "/api/v1/group",
            "/api/v1/access-request",
            "/api/v1/network",
          )
        : invalidate(queryClient, "/api/v1/node"));
      setRunning(null);
      report(action, ids.length - failures.length, failures);
    },
  };
}

/**
 * One request at a time, chained rather than looped, so a selection of forty does not open forty
 * connections at once. A machine that fails does not stop the ones after it; what it said is kept
 * for the report.
 */
async function runInTurn(action: BulkAction, ids: readonly string[]): Promise<string[]> {
  const failures: string[] = [];

  await ids.reduce(
    (chain, id) =>
      chain
        .then(() => callOne(action, id))
        .catch((error: unknown) => {
          failures.push(errorMessage(error));
        }),
    Promise.resolve(),
  );

  return failures;
}

/** One line for the whole run: what worked, and what the first failure said. */
function report(action: BulkAction, done: number, failures: readonly string[]): void {
  const { verb, past } = words[action];

  if (failures.length === 0) {
    toast.success(`${plural(done, "machine")} ${past}`);

    return;
  }

  const failed = `Could not ${verb} ${plural(failures.length, "machine")}`;

  toast.error(
    done === 0 ? failed : `${plural(done, "machine")} ${past}, ${failures.length} failed`,
    failures[0],
  );
}
