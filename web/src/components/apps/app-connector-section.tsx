import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { App, Node } from "~/api/queries.ts";
import { appsForNode } from "~/components/apps/model.ts";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";

/**
 * What the machine serves as an app connector: the apps whose tags it carries, and how many
 * addresses it has learned for each. A machine advertising the connector that no app names is doing
 * nothing, which is what the empty state says.
 */
export function AppConnectorSection({
  node,
  apps,
}: {
  readonly node: Node;
  readonly apps: readonly App[];
}): ReactElement {
  const served = appsForNode(apps, node.id);

  return (
    <Section
      title="App connector"
      description="The machine resolves these apps' domains and advertises a route for every address it learns."
      bodyClassName="p-0"
    >
      {served.length === 0 ? (
        <SectionEmpty
          title="No app names this machine"
          description="The machine advertises the connector, but no app picks up its tags yet."
        />
      ) : (
        served.map((app) => <AppRow key={app.id} app={app} nodeId={node.id} />)
      )}
    </Section>
  );
}

function AppRow({ app, nodeId }: { readonly app: App; readonly nodeId: string }): ReactElement {
  const here = app.nodes.find((node) => node.nodeId === nodeId);
  const learned = here?.learnedRoutes ?? 0;
  const pending = here?.pending ?? 0;

  return (
    <SectionRow className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2.5">
      <div className="flex min-w-0 flex-col gap-0.5">
        <Link
          to="/apps"
          search={{ q: app.name }}
          className="truncate font-medium text-kumo-default outline-none hover:underline focus-visible:ring-2 focus-visible:ring-kumo-focus"
        >
          {app.name}
        </Link>
        {app.domains.length === 0 ? null : (
          <span className="truncate text-xs text-kumo-subtle">{app.domains.join(", ")}</span>
        )}
      </div>
      <span className="shrink-0 text-sm text-kumo-subtle">
        {pending === 0 ? `${learned} learned` : `${learned} learned · ${pending} pending`}
      </span>
    </SectionRow>
  );
}
