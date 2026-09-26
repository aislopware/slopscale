import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import type { ReactElement } from "react";
import { object } from "valibot";

import { trafficReportersQuery, trafficSummaryQuery } from "~/api/traffic.ts";
import type { TrafficScope } from "~/api/traffic.ts";
import { plural } from "~/components/overview/plural.ts";
import { SearchInput } from "~/components/table/search-input.tsx";
import { TableFooter } from "~/components/table/toolbar.tsx";
import { MachinesTable, trafficNodeName } from "~/components/traffic/machines-table.tsx";
import { optionalText, trafficWindowEntries, windowOf } from "~/components/traffic/range.ts";
import type { TrafficWindowSearch } from "~/components/traffic/range.ts";
import { WindowHeader } from "~/components/traffic/window-header.tsx";
import { isRefusedWindow, loadWindow } from "~/components/traffic/window-refusal.tsx";
import { WindowToolbar } from "~/components/traffic/window-toolbar.tsx";
import { Frame } from "~/components/ui/frame.tsx";
import { SectionEmpty } from "~/components/ui/section.tsx";

/** The server's own cap on how many machines one summary lists. */
const allMachines = 100;

const searchSchema = object({ ...trafficWindowEntries, q: optionalText });

function scopeOf(search: TrafficWindowSearch): TrafficScope {
  return { ...windowOf(search), node: "" };
}

export const Route = createFileRoute("/_app/traffic/machines/")({
  validateSearch: searchSchema,
  loaderDeps: ({ search }) => windowOf(search),
  loader: async ({ context, deps }) => {
    const scope = scopeOf(deps);

    await loadWindow([
      context.queryClient.query(trafficSummaryQuery(scope, allMachines)),
      context.queryClient.query(trafficReportersQuery),
    ]);
  },
  component: MachinesPage,
});

function MachinesPage(): ReactElement {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data, error } = useQuery({
    ...trafficSummaryQuery(scopeOf(search), allMachines),
    placeholderData: keepPreviousData,
  });
  const reporters = useQuery(trafficReportersQuery);
  const nodes = data?.nodes ?? [];
  const needle = search.q.trim().toLowerCase();
  const shown =
    needle === ""
      ? nodes
      : nodes.filter((node) => trafficNodeName(node).toLowerCase().includes(needle));
  const whole = data === undefined ? 0 : data.total.txBytes + data.total.rxBytes;

  return (
    <>
      <WindowHeader
        title="Machines"
        description="Every machine that sent traffic through a gateway in the window, busiest first."
        window={data}
        reporters={reporters.data?.reporters ?? []}
        carried={data?.reporters}
        gateway={search.gateway}
      />
      <WindowToolbar
        search={search}
        failure={error}
        reporters={reporters.data?.reporters ?? []}
        onChange={(next) => {
          void navigate({ search: (previous) => ({ ...previous, ...next }) });
        }}
      >
        <SearchInput
          value={search.q}
          placeholder="Search machines"
          onValueChange={(q) => {
            void navigate({ search: (previous) => ({ ...previous, q }), replace: true });
          }}
        />
      </WindowToolbar>
      {isRefusedWindow(error) ? null : (
        <Frame>
          <MachinesTable
            nodes={shown}
            whole={whole}
            search={search}
            empty={
              needle === "" ? undefined : (
                <SectionEmpty
                  title="No machine matches"
                  description={`None of the ${plural(nodes.length, "machine")} in the window is called that.`}
                />
              )
            }
            footer={
              nodes.length === 0 ? undefined : (
                <TableFooter>
                  {shown.length === nodes.length
                    ? `Showing ${plural(nodes.length, "machine")}`
                    : `Showing ${shown.length} of ${plural(nodes.length, "machine")}`}
                </TableFooter>
              )
            }
          />
        </Frame>
      )}
    </>
  );
}
