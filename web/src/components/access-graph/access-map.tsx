import { Popover } from "@cloudflare/kumo/components/popover";
import { cn } from "@cloudflare/kumo/utils";
import { useRef, useState } from "react";
import type { KeyboardEvent, ReactElement } from "react";

import {
  cellLabel,
  clampCell,
  edgeSummary,
  nextCell,
  openness,
} from "~/components/access-graph/model.ts";
import type {
  AccessClass,
  AccessMap,
  MapCell,
  MatrixPos,
  Openness,
} from "~/components/access-graph/model.ts";
import { Section } from "~/components/ui/section.tsx";
import { TagList } from "~/components/ui/tag.tsx";

const openDelay = 150;

/**
 * Three steps of the one tint, mixed from the info colour so both themes get the same scale: the
 * darker the cell, the more the row may open on the column. One hue, because the map asks "how
 * open", not "which kind", and a second hue would ask the eye to keep a legend in mind.
 */
const tints: Record<Openness, string> = {
  all: "bg-[color-mix(in_oklab,var(--color-kumo-info)_28%,transparent)]",
  some: "bg-[color-mix(in_oklab,var(--color-kumo-info)_15%,transparent)]",
  other: "bg-[color-mix(in_oklab,var(--color-kumo-info)_7%,transparent)]",
};

/** A class of one machine against itself: an em dash, which reads as "not applicable". */
const selfMark = "\u2014";

const legend: readonly { readonly open: Openness; readonly label: string }[] = [
  { open: "all", label: "Every port" },
  { open: "some", label: "Some ports" },
  { open: "other", label: "SSH, routes or capabilities only" },
];

/** Separate borders, so the sticky header and column keep lines that collapsing would eat. */
const tableClass = "w-max min-w-full border-separate border-spacing-0 text-base";
const cellClass = "h-10 w-44 border-r border-b border-kumo-hairline p-0 last:border-r-0";
const headerClass = "sticky z-10 bg-kumo-base p-0 text-left font-normal";
const rowHeaderClass = cn(headerClass, "left-0 w-44 border-r border-b border-kumo-line");
const columnHeaderClass = cn(
  headerClass,
  "top-0 min-w-32 border-r border-b border-kumo-line last:border-r-0",
);

/** The one cell in the tab order, found by its place rather than by a ref per cell. */
function focusCell(grid: HTMLTableElement | null, at: MatrixPos): void {
  grid?.querySelector<HTMLElement>(`[data-row="${at.row}"][data-col="${at.col}"]`)?.focus();
}

function countMachines(count: number): string {
  return count === 1 ? "1 machine" : `${count} machines`;
}

/**
 * The whole tailnet at once: a row per class of machines the policy treats alike, a column per
 * class, and in each cell what the row may open on the column, tinted by how much that is. Sixty
 * laptops that all reach the same servers the same way are one row, so the map stays a screen wide
 * at any size of tailnet and a cell has room for its words.
 *
 * The grid is one tab stop: the arrows move the focus between cells and Home and End go to the ends
 * of the row. Hovering a cell picks out its row and column headers, so a cell far from either can
 * still be traced back.
 */
export function AccessMapView({
  map,
  onPick,
}: {
  readonly map: AccessMap;
  /** Picking a machine from a class's list opens its two lists. */
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  const grid = useRef<HTMLTableElement>(null);
  const [focused, setFocused] = useState<MatrixPos>({ row: 0, col: 0 });
  const [hovered, setHovered] = useState<MatrixPos | null>(null);
  const size = map.classes.length;
  const at = clampCell(focused, size);
  const pairs = map.open === 1 ? "1 pair open" : `${map.open} pairs open`;
  const groups = size === 1 ? "1 group" : `${size} groups`;

  const onKeyDown = (event: KeyboardEvent, from: MatrixPos): void => {
    const next = nextCell(event.key, from, size);

    if (next === undefined) {
      return;
    }

    // The cells sit in a container that scrolls both ways; the arrows move the focus, not it.
    event.preventDefault();
    setFocused(next);
    focusCell(grid.current, next);
  };

  return (
    <Section
      title="Who reaches what"
      description={`Rows reach columns. Machines the policy treats alike share a row: ${countMachines(map.machines)} in ${groups}, ${pairs}.`}
      bodyClassName="p-0"
    >
      <Legend />
      <div className="max-h-[40rem] overflow-auto">
        <table ref={grid} className={tableClass}>
          <thead>
            <tr>
              <th scope="col" className={cn(rowHeaderClass, "top-0 z-20")}>
                <span className="sr-only">Source machines</span>
              </th>
              {map.classes.map((group, colIndex) => (
                <th
                  key={group.id}
                  scope="col"
                  className={cn(columnHeaderClass, hovered?.col === colIndex && "bg-kumo-tint")}
                >
                  <ClassHeader group={group} onPick={onPick} />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {map.rows.map((row, rowIndex) => (
              <tr key={row.src.id}>
                <th
                  scope="row"
                  className={cn(rowHeaderClass, hovered?.row === rowIndex && "bg-kumo-tint")}
                >
                  <ClassHeader group={row.src} onPick={onPick} />
                </th>
                {row.cells.map((cell, colIndex) => (
                  <td key={cell.dst.id} className={cellClass}>
                    <Cell
                      cell={cell}
                      src={row.src}
                      at={{ row: rowIndex, col: colIndex }}
                      focused={at.row === rowIndex && at.col === colIndex}
                      onKeyDown={onKeyDown}
                      onHover={setHovered}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  );
}

/** What the tints mean, above the grid so it is read before the cells rather than found under them. */
function Legend(): ReactElement {
  return (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-kumo-hairline px-3 py-2 text-xs text-kumo-subtle">
      {legend.map((item) => (
        <li key={item.open} className="flex items-center gap-1.5">
          <span aria-hidden className={cn("size-3 rounded-xs", tints[item.open])} />
          {item.label}
        </li>
      ))}
      <li className="flex items-center gap-1.5">
        <span aria-hidden className="w-3 text-center leading-none">
          {selfMark}
        </span>
        A machine against itself
      </li>
    </ul>
  );
}

/** The name a class goes by in a sentence: its label, and for several machines which ones. */
function classPhrase(group: AccessClass): string {
  return group.members.length === 1 ? group.label : `${group.label} (${group.detail})`;
}

/**
 * A class on either axis: what its machines have in common and how many there are. Its machines are
 * one hover or tap away, each a link to its own two lists.
 */
function ClassHeader({
  group,
  onPick,
}: {
  readonly group: AccessClass;
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  // A class of one is named by its machine, and a tagged machine's tags are its owner line.
  const only = group.members.length === 1 ? group.members[0] : undefined;

  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        className="flex h-10 w-44 cursor-pointer flex-col justify-center gap-0.5 px-3 text-left outline-none hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:ring-inset"
      >
        {group.tagged ? (
          <TagList tags={group.members[0]?.tags ?? []} size="sm" className="flex-nowrap" />
        ) : (
          <span className="block truncate text-kumo-default">{group.label}</span>
        )}
        {only !== undefined && only.tags.length > 0 ? (
          <TagList tags={only.tags} size="sm" className="flex-nowrap" />
        ) : (
          <span className="block truncate text-xs text-kumo-subtle">{group.detail}</span>
        )}
      </Popover.Trigger>
      <Popover.Content side="bottom" align="start" className="max-w-72 gap-1 p-3">
        <Popover.Title className="text-sm leading-5 font-medium">
          {group.label} · {group.detail}
        </Popover.Title>
        <ul className="-mx-1 flex max-h-64 flex-col overflow-y-auto px-1">
          {group.members.map((node) => (
            <li key={node.id}>
              <button
                type="button"
                className="w-full truncate rounded-sm px-1 py-0.5 text-left text-sm hover:text-kumo-link hover:underline"
                onClick={() => {
                  onPick(node.id);
                }}
              >
                {node.name}
              </button>
            </li>
          ))}
        </ul>
      </Popover.Content>
    </Popover>
  );
}

interface CellProps {
  readonly cell: MapCell;
  readonly src: AccessClass;
  readonly at: MatrixPos;
  /** Whether this is the cell the grid's single tab stop currently sits on. */
  readonly focused: boolean;
  readonly onKeyDown: (event: KeyboardEvent, from: MatrixPos) => void;
  readonly onHover: (at: MatrixPos | null) => void;
}

/**
 * One pair of classes. Every cell is a button, open or not, so the arrows walk a whole grid rather
 * than skipping the gaps; only the focused one is in the tab order.
 */
function Cell({ cell, src, at, focused, onKeyDown, onHover }: CellProps): ReactElement {
  const shared = {
    "data-row": at.row,
    "data-col": at.col,
    tabIndex: focused ? 0 : -1,
    onKeyDown: (event: KeyboardEvent): void => {
      onKeyDown(event, at);
    },
    onMouseEnter: (): void => {
      onHover(at);
    },
    onMouseLeave: (): void => {
      onHover(null);
    },
  };
  const base =
    "flex size-full h-10 items-center px-2 text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:ring-inset";

  if (cell.self || cell.edge === undefined) {
    return (
      <button
        type="button"
        {...shared}
        aria-label={
          cell.self
            ? `${src.label}, one machine against itself`
            : `${classPhrase(src)} does not reach ${classPhrase(cell.dst)}`
        }
        className={cn(base, "justify-center text-kumo-subtle")}
      >
        {cell.self ? <span aria-hidden>{selfMark}</span> : null}
      </button>
    );
  }

  const summary = edgeSummary(cell.edge);

  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        {...shared}
        aria-label={`${classPhrase(src)} reaches ${classPhrase(cell.dst)}: ${summary}`}
        className={cn(base, "cursor-pointer", tints[openness(cell.edge)])}
      >
        <span className="truncate">{cellLabel(cell.edge)}</span>
      </Popover.Trigger>
      <Popover.Content side="top" className="max-w-72 gap-1 p-3">
        <Popover.Title className="text-sm leading-5 font-medium">
          {classPhrase(src)} → {classPhrase(cell.dst)}
        </Popover.Title>
        <p className="text-sm text-kumo-subtle">{summary}</p>
        <p className="text-xs text-kumo-subtle">
          {`Each of ${countMachines(src.members.length)} reaches each of ${countMachines(cell.dst.members.length)} this way.`}
        </p>
      </Popover.Content>
    </Popover>
  );
}
