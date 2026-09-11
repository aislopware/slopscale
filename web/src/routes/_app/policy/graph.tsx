import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { accessGraphQuery } from "~/api/queries.ts";
import type { AccessGraph, AccessGraphNode } from "~/api/schema.gen.ts";
import { AccessMapView } from "~/components/access-graph/access-map.tsx";
import { MachinePicker } from "~/components/access-graph/machine-picker.tsx";
import {
  buildAccessMap,
  fitsMap,
  groupEdges,
  maxMapClasses,
  nodesById,
} from "~/components/access-graph/model.ts";
import { ReachPanel } from "~/components/access-graph/reach-panels.tsx";
import { Callout } from "~/components/ui/callout.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";
import { Section, SectionEmpty } from "~/components/ui/section.tsx";
import { requireScope } from "~/lib/require-scope.ts";

/**
 * The router parses a search value as JSON, so `?node=3` arrives as the number 3 while the id it
 * names is a string. Reading it through this keeps the digits; an absent id means the whole
 * tailnet.
 */
function toNodeId(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value === "" ? undefined : value;
  }

  return typeof value === "number" ? String(value) : undefined;
}

const nodeIdSchema = pipe(unknown(), transform(toNodeId));

const graphSearchSchema = object({ node: optional(nodeIdSchema) });

export const Route = createFileRoute("/_app/policy/graph")({
  validateSearch: graphSearchSchema,
  loaderDeps: ({ search }) => ({ node: search.node ?? "" }),
  beforeLoad: requireScope("policy_file:read"),
  loader: async ({ context, deps }) => {
    await context.queryClient.query(accessGraphQuery(deps.node));
  },
  component: GraphPage,
});

function countMachines(count: number): string {
  return count === 1 ? "1 machine" : `${count} machines`;
}

function GraphPage(): ReactElement {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const nodeId = search.node ?? "";
  const graph = useSuspenseQuery(accessGraphQuery(nodeId)).data;
  const chosen = nodesById(graph.nodes).get(nodeId);

  const setNode = (next: string): void => {
    void navigate({ search: { node: next === "" ? undefined : next }, replace: true });
  };

  return (
    <>
      <PageHeader
        title="Graph"
        description="Who reaches what, read off the rules the server hands each machine rather than off the policy file."
        meta={countMachines(graph.nodes.length)}
        actions={
          <div className="w-56">
            <MachinePicker nodes={graph.nodes} value={nodeId} onValueChange={setNode} />
          </div>
        }
      />
      {graph.enforcing ? null : (
        <Callout
          tone="warning"
          title="No packet filter is in force"
          description="Every machine reaches every other."
        />
      )}
      {chosen === undefined ? (
        <Tailnet graph={graph} onPick={setNode} />
      ) : (
        <Machine graph={graph} node={chosen} />
      )}
    </>
  );
}

/** One machine's two lists: what it may open on others, and what others may open on it. */
function Machine({
  graph,
  node,
}: {
  readonly graph: AccessGraph;
  readonly node: AccessGraphNode;
}): ReactElement {
  const groups = groupEdges(graph.edges, node.id);
  const index = nodesById(graph.nodes);

  return (
    <div className="grid items-start gap-6 xl:grid-cols-2">
      <ReachPanel
        title="Can reach"
        description={`The machines ${node.name} may open something on.`}
        empty="Reaches nothing"
        edges={groups.reachable}
        peer="dst"
        index={index}
      />
      <ReachPanel
        title="Reached by"
        description={`The machines that may open something on ${node.name}.`}
        empty="Nothing reaches it"
        edges={groups.reachedBy}
        peer="src"
        index={index}
      />
    </div>
  );
}

/** The whole tailnet at once, while its classes are few enough to read as a grid. */
function Tailnet({
  graph,
  onPick,
}: {
  readonly graph: AccessGraph;
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  if (graph.nodes.length === 0) {
    return (
      <Section title="Who reaches what" bodyClassName="p-0">
        <SectionEmpty
          title="No machines yet"
          description="The graph is drawn from the machines on the tailnet."
        />
      </Section>
    );
  }

  const map = buildAccessMap(graph.nodes, graph.edges);

  if (!fitsMap(map.classes.length)) {
    return (
      <Section title="Who reaches what" bodyClassName="p-0">
        <SectionEmpty
          title="Too many groups for the map"
          description={`Machines the policy treats alike share a row, and this policy treats ${countMachines(graph.nodes.length)} as ${map.classes.length} groups; a map stops being readable past ${maxMapClasses}. Pick one machine to see what it reaches and what reaches it.`}
        />
      </Section>
    );
  }

  return <AccessMapView map={map} onPick={onPick} />;
}
