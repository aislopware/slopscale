import {
  columnFilteringFeature,
  createExpandedRowModel,
  createFilteredRowModel,
  createPaginatedRowModel,
  createSortedRowModel,
  createTableHook,
  filterFn_arrIncludesSome,
  filterFn_equalsString,
  filterFn_includesString,
  globalFilteringFeature,
  rowExpandingFeature,
  rowPaginationFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_datetime,
  sortFn_text,
  tableFeatures,
} from "@tanstack/react-table";
import type {
  CellData,
  RowData,
  TableFeatures,
  TableOptions,
  TableState,
} from "@tanstack/react-table";

import type {
  AccessRule,
  Group,
  Network,
  Node,
  Posture,
  PostureProvider,
  User,
} from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";

/**
 * Every list in the console sorts and filters client-side: the API returns whole collections and a
 * tailnet has hundreds of rows, not millions.
 */
export const appTableFeatures = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: {
    alphanumeric: sortFn_alphanumeric,
    basic: sortFn_basic,
    datetime: sortFn_datetime,
    text: sortFn_text,
  },
  columnFilteringFeature,
  globalFilteringFeature,
  filteredRowModel: createFilteredRowModel(),
  filterFns: {
    includesString: filterFn_includesString,
    equalsString: filterFn_equalsString,
    arrIncludesSome: filterFn_arrIncludesSome,
  },
  // A table whose rows nest, such as the prefixes on the routes page with the machines advertising
  // them under them, hands the hook `getSubRows`; a flat table has no sub-rows and never expands.
  rowExpandingFeature,
  expandedRowModel: createExpandedRowModel(),
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
});

/** How many rows one page holds. Fewer rows than this and a table never pages at all. */
export const tablePageSize = 50;

const {
  createAppColumnHelper,
  useAppTable: usePagedTable,
  useTableContext,
} = createTableHook({
  features: appTableFeatures,
  globalFilterFn: "includesString",
  enableSortingRemoval: false,
  // Pages narrow their collection inline (`nodes.filter(...)`), so the table is handed a new array on
  // every render and the row model would read that as new data and go back to page one, which would
  // make paging impossible. `DataTable` returns to the first page itself, on the state a page's rows
  // actually depend on.
  autoResetPageIndex: false,
});

export { createAppColumnHelper, useTableContext };

/**
 * A table of one collection. Every table pages at {@link tablePageSize}, because the console renders
 * whole collections and a tailnet's machine list is the one that grows without an operator
 * noticing; `DataTable` puts the paging controls on the band below the panel once there is a second
 * page. A caller that wants every row at once passes `initialState.pagination.pageSize: Infinity`,
 * which the pagination feature reads as a single page.
 */
export function useAppTable<TData extends RowData, TSelected = TableState<typeof appTableFeatures>>(
  tableOptions: Omit<TableOptions<typeof appTableFeatures, TData>, "features">,
  selector?: (state: TableState<typeof appTableFeatures>) => TSelected,
): ReturnType<typeof usePagedTable<TData, TSelected>> {
  return usePagedTable(
    {
      ...tableOptions,
      initialState: {
        ...tableOptions.initialState,
        pagination: {
          pageIndex: 0,
          pageSize: tablePageSize,
          ...tableOptions.initialState?.pagination,
        },
      },
    },
    selector,
  );
}

declare module "@tanstack/react-table" {
  // Rows render actions and names of related records, so the table carries the caller and the
  // collections a cell may need to look one up.
  interface TableMeta<in out TFeatures extends TableFeatures, in out TData extends RowData> {
    me?: Me;
    users?: readonly User[];
    nodes?: readonly Node[];
    groups?: readonly Group[];
    rules?: readonly AccessRule[];
    postures?: readonly Posture[];
    networks?: readonly Network[];
    eventTypes?: readonly string[];
    postureProviders?: readonly PostureProvider[];
    /** Whether the policy file restricts traffic on its own; false means the rules are all there is. */
    policyFileEnforces?: boolean;
    /** Whether the tailnet has a packet filter at all, so a network's protocol and ports apply. */
    enforcing?: boolean;
    /** Whether the server can answer ip:country postures. */
    geoIpAvailable?: boolean;
    /** Whether the caller may approve or deny access requests. */
    canDecide?: boolean;
  }

  // The type parameters must mirror the package's declaration to merge.
  interface ColumnMeta<
    in out TFeatures extends TableFeatures,
    in out TData extends RowData,
    TValue extends CellData = CellData,
  > {
    /** Applied to both the header and body cells of the column by DataTable. */
    className?: string;
    /**
     * A column of numbers: right aligned with tabular figures, so the digits line up down the
     * column.
     */
    numeric?: boolean;
    /**
     * Pins the column to that edge of the table's scroll container, with the row's background under
     * it and a hairline on its inner edge. The row action menu carries this, so it stays reachable
     * on a phone.
     */
    sticky?: "left" | "right";
  }
}
