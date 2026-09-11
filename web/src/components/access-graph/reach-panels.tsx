import { Button } from "@cloudflare/kumo/components/button";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { AccessGraphEdge, AccessGraphNode } from "~/api/schema.gen.ts";
import { edgeListView, edgeRows, portLines, sshLogins } from "~/components/access-graph/model.ts";
import type { Peer } from "~/components/access-graph/model.ts";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";
import { Note, Status } from "~/components/ui/status.tsx";
import { TagList } from "~/components/ui/tag.tsx";
import { ValueList } from "~/components/ui/value-list.tsx";

/** One side of a machine's access: the edges it is the source of, or the ones it is the target of. */
export interface ReachPanelProps {
  readonly title: string;
  readonly description: string;
  readonly empty: string;
  readonly edges: readonly AccessGraphEdge[];
  /** Which end of the edge is the other machine. */
  readonly peer: Peer;
  readonly index: ReadonlyMap<string, AccessGraphNode>;
}

export function ReachPanel({
  title,
  description,
  empty,
  edges,
  peer,
  index,
}: ReachPanelProps): ReactElement {
  const [expanded, setExpanded] = useState(false);
  const rows = edgeRows(edges, peer, index);
  const view = edgeListView(rows, expanded);

  return (
    // The count belongs beside the title: the two panels sit side by side and the shorter one is
    // not the one with fewer machines until you have counted both lists.
    <Section title={`${title} · ${rows.length}`} description={description} bodyClassName="p-0">
      {rows.length === 0 ? <SectionEmpty title={empty} /> : null}
      {view.rows.map((row) => (
        <EdgeRow
          key={`${row.edge.src}>${row.edge.dst}`}
          edge={row.edge}
          node={row.node}
          nodeId={row.nodeId}
        />
      ))}
      {view.hidden === 0 ? null : (
        <SectionRow>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setExpanded(true);
            }}
          >
            {`Show all ${rows.length}`}
          </Button>
        </SectionRow>
      )}
    </Section>
  );
}

function EdgeRow({
  edge,
  node,
  nodeId,
}: {
  readonly edge: AccessGraphEdge;
  readonly node: AccessGraphNode | undefined;
  readonly nodeId: string;
}): ReactElement {
  const logins = sshLogins(edge.sshUsers);

  return (
    <SectionRow className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <Link
          to="/machines/$nodeId"
          params={{ nodeId }}
          className="truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline"
        >
          {node?.name ?? `#${nodeId}`}
        </Link>
        {node === undefined ? null : (
          <Status tone={node.online ? "success" : "neutral"} className="text-xs text-kumo-subtle">
            {node.online ? "Online" : "Offline"}
          </Status>
        )}
      </div>
      <Owner node={node} />
      <dl className="flex flex-col gap-1.5">
        <Fact label="Ports">
          <ValueList items={portLines(edge.ports)} mono empty="None" />
        </Fact>
        {edge.routes.length === 0 ? null : (
          <Fact label="Routes">
            <ValueList items={edge.routes} mono empty="None" />
          </Fact>
        )}
        {logins === "" ? null : (
          <Fact label="SSH">
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <span className="text-kumo-default">{logins}</span>
              {edge.sshCheck ? <Note className="text-xs">Asks for a fresh sign-in</Note> : null}
            </span>
          </Fact>
        )}
        {edge.capabilities.length === 0 ? null : (
          <Fact label="Capabilities">
            <ValueList items={edge.capabilities} mono empty="None" />
          </Fact>
        )}
      </dl>
    </SectionRow>
  );
}

/** Who the machine belongs to: its tags, or its owner's login. */
function Owner({ node }: { readonly node: AccessGraphNode | undefined }): ReactElement | null {
  if (node === undefined) {
    return null;
  }

  if (node.tags.length > 0) {
    return <TagList tags={node.tags} size="sm" />;
  }

  return (
    <span className="text-xs text-kumo-subtle">{node.user === "" ? "No owner" : node.user}</span>
  );
}

function Fact({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <dt className="w-24 shrink-0 text-xs text-kumo-subtle">{label}</dt>
      <dd className="min-w-0 flex-1">{children}</dd>
    </div>
  );
}
