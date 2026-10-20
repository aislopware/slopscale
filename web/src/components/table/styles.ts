import { cn } from "@cloudflare/kumo/utils";

/**
 * The Cloudflare dashboard's DataTable (variant A of the design spec: Domains, Security rules),
 * applied to Kumo's Table parts. The header row sits on the canvas colour in 13px medium subtle
 * text, with a half-height line between columns; body rows are at least 44px with a line on top,
 * 16px in from either side, and take the overlay fill under the pointer. It goes on the `Table`
 * inside a `TableScroll`, which draws the card around it. Borders are separate so a sticky cell
 * keeps its own.
 */
export const tableClass = cn(
  "border-separate border-spacing-0",
  // No `relative` here: Kumo's Table.Head sets it, and a descendant rule would outrank the
  // `sticky` of a pinned header cell, leaving the ⋯ column's heading to scroll away from its cells.
  "[&_th]:h-11 [&_th]:border-b-0 [&_th]:bg-kumo-canvas [&_th]:px-4 [&_th]:py-2",
  "[&_th]:align-middle [&_th]:text-sm [&_th]:font-medium [&_th]:whitespace-nowrap [&_th]:text-kumo-subtle",
  // The column divider: a tick half the header's height, at the cell's right edge.
  "[&_th:not(:last-child)]:after:absolute [&_th:not(:last-child)]:after:top-1/4 [&_th:not(:last-child)]:after:right-0",
  "[&_th:not(:last-child)]:after:h-1/2 [&_th:not(:last-child)]:after:w-px [&_th:not(:last-child)]:after:bg-kumo-line",
  "[&_th]:before:to-kumo-canvas",
  "[&_td]:h-11 [&_td]:border-t [&_td]:border-kumo-line [&_td]:px-4 [&_td]:py-2",
);

/**
 * A body row: the base fill, the overlay fill under the pointer and no stripe. Kumo's default row
 * stripes every second row; the `even` modifiers outrank the stripe instead of relying on source
 * order. The row's fill is also handed to a sticky cell through `--kumo-table-row-bg`.
 */
export const tableRowClass = cn(
  "bg-kumo-base [--kumo-table-row-bg:var(--color-kumo-base)]",
  "even:bg-kumo-base even:[--kumo-table-row-bg:var(--color-kumo-base)]",
  "hover:bg-kumo-overlay hover:[--kumo-table-row-bg:var(--color-kumo-overlay)]",
  "even:hover:bg-kumo-overlay even:hover:[--kumo-table-row-bg:var(--color-kumo-overlay)]",
);

/**
 * A pinned column: Kumo's sticky cell fades its fill over the columns it covers; the dashboard's
 * fade is 16px, and on the header it fades to the header's canvas fill rather than the body's.
 */
export const pinnedEdgeClass = "before:-left-4 before:w-4";

/** The pinned actions column itself: 50px, the ⋯ button centred in it. */
export const actionsColumnClass = "w-[50px] px-3 text-center";

/**
 * The link to a row's own page in its primary cell: underlined in the text's own colour, not blue,
 * as the dashboard's domain and rule names are.
 */
export const rowLinkClass =
  "underline decoration-kumo-subtle/60 underline-offset-2 text-inherit hover:decoration-current focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none";

/** The second line of a two-line cell: 12px subtle, cut with an ellipsis. */
export const cellSecondaryClass = "truncate text-xs text-kumo-subtle";

/** A time in a cell: Paper Mono at 12px with tabular figures, subtle. */
export const cellTimeClass = "font-mono text-xs text-kumo-subtle tabular-nums";
