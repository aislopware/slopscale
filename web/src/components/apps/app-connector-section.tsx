import { Button } from "@cloudflare/kumo/components/button";
import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactElement } from "react";

import { nodeAppConnectorRoutesQuery } from "~/api/queries.ts";
import type { App, Node } from "~/api/queries.ts";
import { appsForNode, learnedRoutes } from "~/components/apps/model.ts";
import type { LearnedRoute } from "~/components/apps/model.ts";
import { plural } from "~/components/overview/plural.ts";
import { createAppColumnHelper, useAppTable } from "~/components/table/app-table.tsx";
import { DataTable } from "~/components/table/data-table.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { Section, SectionEmpty, SectionRow } from "~/components/ui/section.tsx";

const buttonIconSize = 12;

/**
 * What the machine serves as an app connector: the apps whose tags it carries, how many addresses
 * it has learned for each, and the addresses themselves as the client resolved them.
 */
export function AppConnectorSection({
  node,
  apps,
}: {
  readonly node: Node;
  readonly apps: readonly App[];
}): ReactElement {
  return (
    <>
      <AppsSection node={node} apps={apps} />
      <LearnedRoutesSection node={node} />
    </>
  );
}

/**
 * The apps that name this machine. A machine advertising the connector that no app names is doing
 * nothing, which is what the empty state says.
 */
function AppsSection({
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

const helper = createAppColumnHelper<LearnedRoute>();

const columns = helper.columns([
  helper.display({
    id: "domain",
    header: "Domain",
    cell: ({ row }) => <span className="font-mono">{row.original.domain}</span>,
  }),
  helper.display({
    id: "addresses",
    header: "Addresses",
    cell: ({ row }) => <Addresses addresses={row.original.addresses} />,
  }),
]);

/**
 * The addresses the connector resolved, asked of the machine itself rather than read off the
 * server: the client keeps the answers and the routes it advertises follow from them, so this is
 * the only place that says what it currently believes.
 */
function LearnedRoutesSection({ node }: { readonly node: Node }): ReactElement {
  const routes = useQuery({
    ...nodeAppConnectorRoutesQuery(node.id),
    enabled: node.online,
  });
  const rows = learnedRoutes(routes.data?.domains ?? {});
  const table = useAppTable({ data: rows, columns, getRowId: (row) => row.domain });

  return (
    <Section
      title="Learned routes"
      description="The addresses the machine has resolved for the domains it answers for."
      actions={
        node.online ? (
          <Button
            variant="ghost"
            shape="square"
            size="sm"
            aria-label="Ask the machine again"
            loading={routes.isFetching}
            icon={<ArrowsClockwiseIcon size={buttonIconSize} />}
            onClick={() => {
              void routes.refetch();
            }}
          />
        ) : undefined
      }
      panel={false}
    >
      <table.AppTable>
        <DataTable
          empty={
            <SectionEmpty
              title="Nothing learned yet"
              description={
                node.online
                  ? "The machine resolves a domain the first time something asks for it."
                  : "Connect the machine to see what it resolved."
              }
            />
          }
          footer={
            rows.length === 0 ? undefined : (
              <TableFooter>{`Showing ${plural(rows.length, "domain")}`}</TableFooter>
            )
          }
        />
      </table.AppTable>
    </Section>
  );
}

function Addresses({ addresses }: { readonly addresses: readonly string[] }): ReactElement {
  if (addresses.length === 0) {
    return <span className="text-kumo-subtle">None yet</span>;
  }

  return <span className="font-mono">{addresses.join(", ")}</span>;
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
