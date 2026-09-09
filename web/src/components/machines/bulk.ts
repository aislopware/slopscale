import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { fetchClient } from "~/api/client.ts";
import { errorMessage } from "~/api/error.ts";
import { invalidate } from "~/api/queries.ts";
import type { Node } from "~/api/queries.ts";
import type { NodeClientUpdateResult } from "~/api/schema.gen.ts";
import { plural } from "~/components/overview/plural.ts";
import { toast } from "~/components/ui/toast.ts";
import { nodeName } from "~/lib/node.ts";

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

/**
 * The machines a bulk client update would reach: ticked, connected and behind a newer release. The
 * request is a live round trip to each client, so asking an offline or up-to-date machine would
 * only collect a refusal.
 */
export function outdatedSelection(nodes: readonly Node[], selected: ReadonlySet<string>): string[] {
  return nodes
    .filter((node) => selected.has(node.id) && node.online && node.updateAvailable)
    .map((node) => node.id);
}

/** One machine that did not take the update on, and what its client said about it. */
export interface ClientUpdateRefusal {
  readonly nodeId: string;
  readonly name: string;
  readonly message: string;
}

/** How a whole run went: how many clients started, and what the rest said. */
export interface ClientUpdateOutcome {
  readonly started: number;
  readonly refused: readonly ClientUpdateRefusal[];
}

/**
 * The per-machine answers as a run: the server answers 200 with one result per machine, so a
 * refusal is a fact in the body rather than a failed request, and each carries the client's own
 * words.
 */
export function summariseClientUpdates(
  results: readonly NodeClientUpdateResult[],
  nodes: readonly Node[],
): ClientUpdateOutcome {
  const refused = results
    .filter((result) => !result.started)
    .map((result) => ({
      nodeId: result.nodeId,
      name: nameOf(nodes, result.nodeId),
      // The server sends the field empty rather than leaving it out when the client said nothing.
      message: refusalMessage(result.error),
    }));

  return { started: results.length - refused.length, refused };
}

function refusalMessage(error: string | undefined): string {
  return error === undefined || error === "" ? "The client gave no reason." : error;
}

function nameOf(nodes: readonly Node[], nodeId: string): string {
  const node = nodes.find((candidate) => candidate.id === nodeId);

  return node === undefined ? `#${nodeId}` : nodeName(node);
}

/** "Started on 3 machines", "Started on 3, 1 refused", "2 machines refused". */
export function clientUpdateSummary(outcome: ClientUpdateOutcome): string {
  if (outcome.refused.length === 0) {
    return `Started on ${plural(outcome.started, "machine")}`;
  }

  if (outcome.started === 0) {
    return `${plural(outcome.refused.length, "machine")} refused`;
  }

  return `Started on ${String(outcome.started)}, ${String(outcome.refused.length)} refused`;
}

export interface ClientUpdateRunner {
  readonly running: boolean;
  /** The last run's answers, kept so the bar can offer the refusals; null before the first run. */
  readonly outcome: ClientUpdateOutcome | null;
  readonly run: (nodes: readonly Node[], ids: readonly string[]) => Promise<void>;
  readonly clearOutcome: () => void;
}

/**
 * Starts a client update on a whole selection. Unlike the other bulk actions this is one request:
 * the server walks the machines a few at a time and answers with one result each, so the console
 * never opens forty control-plane round trips of its own.
 */
export function useClientUpdateBulk(): ClientUpdateRunner {
  const queryClient = useQueryClient();
  const [running, setRunning] = useState(false);
  const [outcome, setOutcome] = useState<ClientUpdateOutcome | null>(null);

  return {
    running,
    outcome,
    clearOutcome: () => {
      setOutcome(null);
    },
    run: async (nodes, ids) => {
      setRunning(true);

      // No `finally`: the catch swallows the failure, so the last lines always run, and the React
      // Compiler cannot lower a try statement that has one.
      let results: NodeClientUpdateResult[] | null = null;

      try {
        const { data } = await fetchClient.POST("/api/v1/nodes/client-update", {
          body: { nodeIds: [...ids] },
        });

        results = data?.results ?? [];
      } catch (error: unknown) {
        toast.error("Could not start the updates", error);
      }

      await invalidate(queryClient, "/api/v1/node");
      setRunning(false);

      if (results !== null) {
        reportClientUpdates(results, nodes, setOutcome);
      }
    },
  };
}

/** The run in one line, and the refusals kept for the details dialog. */
function reportClientUpdates(
  results: readonly NodeClientUpdateResult[],
  nodes: readonly Node[],
  keep: (outcome: ClientUpdateOutcome) => void,
): void {
  const summary = summariseClientUpdates(results, nodes);

  keep(summary);

  if (summary.refused.length === 0) {
    toast.success(clientUpdateSummary(summary));

    return;
  }

  toast.error(clientUpdateSummary(summary), summary.refused[0]?.message);
}
