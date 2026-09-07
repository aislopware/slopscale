import {
  columnFilteringFeature,
  createFilteredRowModel,
  createSortedRowModel,
  createTableHook,
  filterFn_arrIncludesSome,
  filterFn_equalsString,
  filterFn_includesString,
  globalFilteringFeature,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_datetime,
  sortFn_text,
  tableFeatures,
} from "@tanstack/react-table";
import type { CellData, RowData, TableFeatures } from "@tanstack/react-table";

import type { AccessRule, Group, Network, Node, User } from "~/api/queries.ts";
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
});

export const { createAppColumnHelper, useAppTable, useTableContext } = createTableHook({
  features: appTableFeatures,
  globalFilterFn: "includesString",
  enableSortingRemoval: false,
});

declare module "@tanstack/react-table" {
  // Rows render actions and names of related records, so the table carries the caller and the
  // collections a cell may need to look one up.
  interface TableMeta<in out TFeatures extends TableFeatures, in out TData extends RowData> {
    me?: Me;
    users?: readonly User[];
    nodes?: readonly Node[];
    groups?: readonly Group[];
    rules?: readonly AccessRule[];
    networks?: readonly Network[];
    eventTypes?: readonly string[];
    /** Whether the policy file restricts traffic on its own; false means the rules are all there is. */
    policyFileEnforces?: boolean;
  }

  // The type parameters must mirror the package's declaration to merge.
  interface ColumnMeta<
    in out TFeatures extends TableFeatures,
    in out TData extends RowData,
    TValue extends CellData = CellData,
  > {
    /** Applied to both the header and body cells of the column by DataTable. */
    className?: string;
  }
}
