import { Popover } from "@cloudflare/kumo/components/popover";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import { useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, ReactElement } from "react";

import {
  ClassHeader,
  ColumnLabel,
  focusClass,
  openDelay,
} from "~/components/access-graph/class-header.tsx";
import {
  cellLabel,
  cellSummary,
  clampCell,
  edgeSummary,
  mapDensity,
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

/** The hue of Kumo's info colour, so the ramp sits with the links rather than beside them. */
const rampHue = 237;

/**
 * Three steps of one hue, a pair per theme: the darker the cell, the more the row may open on the
 * column. One hue, because the map asks "how open", not "which kind", and a second hue would ask
 * the eye to keep a legend in mind. The steps are set in OKLCH rather than mixed from a token so
 * that they are a lightness apart on both themes; mixed into a dark surface, three tints of blue
 * came out as three navies.
 */
const tints: Record<Openness | "closed", CSSProperties> = {
  closed: { backgroundColor: "light-dark(oklch(96% 0 0), oklch(22% 0 0))" },
  other: {
    backgroundColor: `light-dark(oklch(95% 0.03 ${rampHue}), oklch(27% 0.035 ${rampHue}))`,
  },
  some: {
    backgroundColor: `light-dark(oklch(88% 0.07 ${rampHue}), oklch(34% 0.07 ${rampHue}))`,
  },
  all: {
    backgroundColor: `light-dark(oklch(80% 0.11 ${rampHue}), oklch(45% 0.11 ${rampHue}))`,
  },
};

/**
 * A closed pair is a filled cell too, one step off the surface on either theme, so the grid reads
 * as a grid and a gap in it never looks like a cell that failed to draw. A machine against itself
 * is hatched: not closed, not open, nothing to say.
 */
const selfClass =
  "bg-[repeating-linear-gradient(135deg,var(--color-kumo-hairline)_0_1px,transparent_1px_6px)]";

const legend: readonly { readonly open: Openness | "closed"; readonly label: string }[] = [
  { open: "closed", label: "Closed" },
  { open: "other", label: "SSH, routes or capabilities only" },
  { open: "some", label: "Some ports" },
  { open: "all", label: "Every port" },
];

/** Separate borders, so the sticky header and column keep lines that collapsing would eat. */
const tableClass = "w-max border-separate border-spacing-0";
const headerClass = "sticky z-10 bg-kumo-base p-0 text-left align-bottom font-normal";
const rowHeaderClass = cn(headerClass, "left-0 border-r border-kumo-hairline");
const columnHeaderClass = cn(headerClass, "top-0 border-b border-kumo-hairline");

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
 * at any size of tailnet.
 *
 * Up to eight classes a cell is a tile with words in it. Past that the tiles would run off the
 * screen, so a cell is a bare square, the grid fits on one screen, and the words of the cell under
 * the pointer, or the focused one, are read out on the line under the map.
 *
 * The grid is one tab stop: the arrows move the focus between cells and Home and End go to the ends
 * of the row. Hovering a cell picks out its row and column and steps the rest back, so a cell far
 * from either header can still be traced to both.
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
  const dense = mapDensity(size) === "squares";
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
      actions={<Legend />}
      bodyClassName="p-0"
      footer={dense ? <Readout map={map} at={hovered ?? at} /> : undefined}
    >
      <div className="max-h-[40rem] overflow-auto">
        <table ref={grid} className={tableClass}>
          <thead>
            <tr>
              <th scope="col" className={cn(rowHeaderClass, columnHeaderClass, "z-20")}>
                <span className="sr-only">Source machines</span>
              </th>
              {map.classes.map((group, colIndex) => (
                <th
                  key={group.id}
                  scope="col"
                  className={cn(columnHeaderClass, hovered?.col === colIndex && "bg-kumo-tint")}
                >
                  {dense ? (
                    <ColumnLabel group={group} onPick={onPick} />
                  ) : (
                    <ClassHeader group={group} onPick={onPick} className="w-40 px-3 py-2" />
                  )}
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
                  <ClassHeader
                    group={row.src}
                    onPick={onPick}
                    dense={dense}
                    className={cn("pr-4 pl-5", dense ? "max-w-72 py-0.5" : "w-44 py-1")}
                  />
                </th>
                {row.cells.map((cell, colIndex) => (
                  <td
                    key={cell.dst.id}
                    className={cn(
                      "p-0.5",
                      colIndex === 0 && "pl-1.5",
                      colIndex === size - 1 && "pr-1.5",
                      rowIndex === 0 && "pt-1.5",
                      rowIndex === size - 1 && "pb-1.5",
                    )}
                  >
                    <Cell
                      cell={cell}
                      src={row.src}
                      at={{ row: rowIndex, col: colIndex }}
                      focused={at.row === rowIndex && at.col === colIndex}
                      dense={dense}
                      // A hovered cell steps back every cell not in its row or column.
                      dimmed={
                        hovered !== null && hovered.row !== rowIndex && hovered.col !== colIndex
                      }
                      onKeyDown={onKeyDown}
                      onFocus={setFocused}
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

/**
 * The ramp, on the band beside the title: closed to every port, and the hatch for a machine against
 * itself. Each step names itself on hover and to a screen reader.
 */
function Legend(): ReactElement {
  return (
    <ul className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-kumo-subtle">
      <li className="flex items-center gap-1.5">
        <span aria-hidden>Closed</span>
        <span className="flex gap-0.5">
          {legend.map((step) => (
            <Tooltip key={step.open} content={step.label}>
              <span className="block size-3 rounded-xs" style={tints[step.open]}>
                <span className="sr-only">{step.label}</span>
              </span>
            </Tooltip>
          ))}
        </span>
        <span aria-hidden>Every port</span>
      </li>
      <li className="flex items-center gap-1.5">
        <span aria-hidden className={cn("size-3 rounded-xs", selfClass)} />A machine against itself
      </li>
    </ul>
  );
}

/** The name a class goes by in a sentence: its label, and for several machines which ones. */
function classPhrase(group: AccessClass): string {
  return group.members.length === 1 ? group.label : `${group.label} (${group.detail})`;
}

/**
 * The words of the cell under the pointer, or of the focused one, on the line under a map whose
 * cells are too small to hold them: the pair, and everything the row opens on the column.
 */
function Readout({ map, at }: { readonly map: AccessMap; readonly at: MatrixPos }): ReactElement {
  const row = map.rows[at.row];
  const cell = row?.cells[at.col];

  return (
    <p aria-live="polite" className="flex min-h-lh flex-wrap items-baseline gap-x-2 gap-y-0.5">
      {row === undefined || cell === undefined ? null : (
        <>
          <ClassName group={row.src} />
          <span aria-hidden className="text-kumo-subtle">
            →
          </span>
          <span className="sr-only">reaches</span>
          <ClassName group={cell.dst} />
          <span className="ml-auto text-kumo-strong">{cellSummary(cell)}</span>
        </>
      )}
    </p>
  );
}

function ClassName({ group }: { readonly group: AccessClass }): ReactElement {
  return (
    <span className="flex items-baseline gap-x-1.5">
      <span className="font-medium text-kumo-default">{group.label}</span>
      {group.members.length === 1 ? null : (
        <span className="text-xs text-kumo-subtle">{group.detail}</span>
      )}
    </span>
  );
}

interface CellProps {
  readonly cell: MapCell;
  readonly src: AccessClass;
  readonly at: MatrixPos;
  /** Whether this is the cell the grid's single tab stop currently sits on. */
  readonly focused: boolean;
  /** A bare square rather than a tile with words in it. */
  readonly dense: boolean;
  /** Whether another cell is hovered and this one is in neither its row nor its column. */
  readonly dimmed: boolean;
  readonly onKeyDown: (event: KeyboardEvent, from: MatrixPos) => void;
  readonly onFocus: (at: MatrixPos) => void;
  readonly onHover: (at: MatrixPos | null) => void;
}

/**
 * One pair of classes. Every cell is a button, open or not, so the arrows walk a whole grid rather
 * than skipping the gaps; only the focused one is in the tab order. A tile carries its words and
 * the rest on hover; a square carries nothing, the readout under the map does.
 */
function Cell({
  cell,
  src,
  at,
  focused,
  dense,
  dimmed,
  onKeyDown,
  onFocus,
  onHover,
}: CellProps): ReactElement {
  const shared = {
    "data-row": at.row,
    "data-col": at.col,
    tabIndex: focused ? 0 : -1,
    onKeyDown: (event: KeyboardEvent): void => {
      onKeyDown(event, at);
    },
    onFocus: (): void => {
      onFocus(at);
    },
    onMouseEnter: (): void => {
      onHover(at);
    },
    onMouseLeave: (): void => {
      onHover(null);
    },
  };
  const base = cn(
    "flex items-center text-left text-sm text-kumo-default hover:ring hover:ring-kumo-contrast hover:ring-inset",
    dense ? "size-6 rounded-sm" : "h-9 w-40 rounded-md px-2.5",
    dimmed && "opacity-40",
    focusClass,
  );

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
        className={cn(base, cell.self && selfClass)}
        style={cell.self ? undefined : tints.closed}
      />
    );
  }

  const summary = edgeSummary(cell.edge);
  const label = `${classPhrase(src)} reaches ${classPhrase(cell.dst)}: ${summary}`;

  if (dense) {
    return (
      <button
        type="button"
        {...shared}
        aria-label={label}
        className={base}
        style={tints[openness(cell.edge)]}
      />
    );
  }

  return (
    <Popover>
      <Popover.Trigger
        openOnHover
        delay={openDelay}
        {...shared}
        aria-label={label}
        className={cn(base, "cursor-pointer")}
        style={tints[openness(cell.edge)]}
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
