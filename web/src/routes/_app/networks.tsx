import { Tabs } from "@cloudflare/kumo/components/tabs";
import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";
import { object, optional, pipe, transform, unknown } from "valibot";

import { groupsQuery, networksQuery, nodesQuery } from "~/api/queries.ts";
import { NetworksTab } from "~/components/networks/networks-tab.tsx";
import { RoutesTab } from "~/components/networks/routes-tab.tsx";
import { PageHeader } from "~/components/ui/page-header.tsx";

const tabs = ["networks", "routes"] as const;
type Tab = (typeof tabs)[number];

const tabItems: readonly { value: Tab; label: string }[] = [
  { value: "networks", label: "Networks" },
  { value: "routes", label: "Routes" },
];

function toText(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function toTab(value: unknown): Tab | undefined {
  return tabs.find((known) => known === value);
}

const anyValue = unknown();
const optionalTab = optional(pipe(anyValue, transform(toTab)));
const optionalText = optional(pipe(anyValue, transform(toText)));

/** Absent means the default, so the URL only carries a tab or search the operator chose. */
const searchSchema = object({ tab: optionalTab, q: optionalText });

export const Route = createFileRoute("/_app/networks")({
  validateSearch: searchSchema,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.query(networksQuery),
      context.queryClient.query(groupsQuery),
      context.queryClient.query(nodesQuery),
    ]);
  },
  component: NetworksPage,
});

function NetworksPage(): ReactElement {
  const { me } = Route.useRouteContext();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { networks, enforcing } = useSuspenseQuery(networksQuery).data;
  const { groups } = useSuspenseQuery(groupsQuery).data;
  const { nodes } = useSuspenseQuery(nodesQuery).data;
  const tab = search.tab ?? "networks";
  const text = search.q ?? "";

  const setTab = (value: string): void => {
    const next = toTab(value) ?? "networks";

    void navigate({
      search: () => ({ tab: next === "networks" ? undefined : next, q: undefined }),
    });
  };
  const setSearch = (value: string): void => {
    void navigate({
      search: (current) => ({ ...current, q: value === "" ? undefined : value }),
      replace: true,
    });
  };

  return (
    <>
      <PageHeader
        title="Networks"
        description="Subnets and exit nodes reached through routing machines. A network hands its routes only to the groups you pick; the routes tab shows everything any machine advertises."
        meta={describe(networks, nodes)}
      />
      <div className="flex">
        <Tabs variant="segmented" tabs={[...tabItems]} value={tab} onValueChange={setTab} />
      </div>
      {/* Kumo's Tabs renders the controls only, so each body names itself as the panel. */}
      {tab === "networks" ? (
        <TabPanel label="Networks">
          <NetworksTab
            me={me}
            networks={networks}
            enforcing={enforcing}
            groups={groups}
            nodes={nodes}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
      {tab === "routes" ? (
        <TabPanel label="Routes">
          <RoutesTab
            me={me}
            nodes={nodes}
            networks={networks}
            search={text}
            onSearchChange={setSearch}
          />
        </TabPanel>
      ) : null}
    </>
  );
}

function TabPanel({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div role="tabpanel" aria-label={label} className="flex flex-col gap-6">
      {children}
    </div>
  );
}

function describe(
  networks: readonly { enabled: boolean }[],
  nodes: readonly { availableRoutes: string[]; approvedRoutes: string[] }[],
): string {
  const enabled = networks.filter((network) => network.enabled).length;
  const pending = nodes.reduce(
    (sum, node) =>
      sum + node.availableRoutes.filter((route) => !node.approvedRoutes.includes(route)).length,
    0,
  );
  const networkText = enabled === 1 ? "1 network enabled" : `${enabled} networks enabled`;
  const pendingText = pending === 1 ? "1 route pending" : `${pending} routes pending`;

  return `${networkText} · ${pendingText}`;
}
