import { Badge } from "@cloudflare/kumo/components/badge";
import type { ReactElement } from "react";

import type { Posture } from "~/api/queries.ts";
import { PostureMenu } from "~/components/access/posture-menu.tsx";
import { rulesUsingPosture, scheduleSummary } from "~/components/access/posture-model.ts";
import { createAppColumnHelper } from "~/components/table/app-table.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";

const helper = createAppColumnHelper<Posture>();

export const postureColumns = helper.columns([
  helper.accessor((posture) => `${posture.name} ${posture.description}`, {
    id: "name",
    header: "Posture",
    enableSorting: true,
    cell: ({ row }) => <NameCell posture={row.original} />,
    meta: { className: "w-[28%] min-w-48" },
  }),
  helper.accessor((posture) => posture.expressions.join(" "), {
    id: "expressions",
    header: "Conditions",
    enableSorting: false,
    cell: ({ row }) => <Expressions posture={row.original} />,
    meta: { className: "min-w-56" },
  }),
  helper.accessor((posture) => scheduleSummary(posture.schedule), {
    id: "schedule",
    header: "Schedule",
    enableSorting: true,
    cell: ({ row }) => {
      const text = scheduleSummary(row.original.schedule);

      return text === "" ? (
        <span className="text-kumo-inactive">Always</span>
      ) : (
        <span className="whitespace-nowrap">{text}</span>
      );
    },
    meta: { className: "hidden whitespace-nowrap md:table-cell" },
  }),
  helper.accessor((posture) => posture.id, {
    id: "rules",
    header: "Rules",
    enableSorting: false,
    enableGlobalFilter: false,
    cell: ({ row, table }) => {
      const count = rulesUsingPosture(table.options.meta?.rules ?? [], row.original).length;

      return count === 0 ? (
        <span className="text-kumo-inactive">—</span>
      ) : (
        <span className="tabular-nums">{count}</span>
      );
    },
    meta: { className: "whitespace-nowrap" },
  }),
  helper.accessor((posture) => posture.updatedAt, {
    id: "updated",
    header: "Updated",
    enableSorting: true,
    enableGlobalFilter: false,
    sortDescFirst: true,
    cell: ({ row }) => (
      <span className="text-kumo-subtle">
        <RelativeTime value={row.original.updatedAt} />
      </span>
    ),
    meta: { className: "hidden whitespace-nowrap lg:table-cell" },
  }),
  helper.display({
    id: "actions",
    header: "",
    cell: ({ row, table }) => {
      const { me, rules, geoIpAvailable } = table.options.meta ?? {};

      return me === undefined ? null : (
        <PostureMenu
          posture={row.original}
          rules={rules ?? []}
          geoIpAvailable={geoIpAvailable ?? false}
          me={me}
        />
      );
    },
    meta: { className: "w-12 text-right" },
  }),
]);

function NameCell({ posture }: { readonly posture: Posture }): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="truncate font-medium text-kumo-default">{posture.name}</span>
      {posture.description === "" ? null : (
        <span className="truncate text-xs text-kumo-subtle">{posture.description}</span>
      )}
    </div>
  );
}

function Expressions({ posture }: { readonly posture: Posture }): ReactElement {
  if (posture.expressions.length === 0) {
    return <span className="text-kumo-inactive">Schedule only</span>;
  }

  return (
    <div className="flex flex-wrap gap-1">
      {posture.expressions.map((expression) => (
        <Badge key={expression} variant="secondary" className="max-w-72">
          <span className="truncate font-mono">{expression}</span>
        </Badge>
      ))}
    </div>
  );
}
