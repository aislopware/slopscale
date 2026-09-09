import { Button } from "@cloudflare/kumo/components/button";
import { Popover } from "@cloudflare/kumo/components/popover";
import { cn } from "@cloudflare/kumo/utils";
import { useRef, useState } from "react";
import type { KeyboardEvent, ReactElement } from "react";

import type { AccessGraphNode } from "~/api/schema.gen.ts";
import { clampCell, edgeSummary, nextCell, ownerLabel } from "~/components/access-graph/model.ts";
import type { Matrix, MatrixCell, MatrixPos } from "~/components/access-graph/model.ts";
import { Section } from "~/components/ui/section.tsx";

const openDelay = 150;

/** Both sticky axes need an opaque background and a line, or the cells scroll through them. */
const headerCellClass = "sticky top-0 z-10 bg-kumo-base p-0 align-bottom";
/** Separate borders, so the sticky header and column keep lines that collapsing would eat. */
const tableClass = "w-max border-separate border-spacing-0 text-base";

/** A hairline grid, so a filled square can be traced back to its row and its column. */
const cellClass = "border-r border-b border-kumo-hairline p-0";

const rowHeaderClass =
  "sticky left-0 z-10 border-r border-b border-kumo-hairline border-r-kumo-line bg-kumo-base px-3 py-0 text-left font-normal";

/** Every cell is the same square, filled or not, so the grid reads as a grid. */
const squareClass =
  "flex size-6 items-center justify-center outline-none focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:ring-inset";

/** The one cell in the tab order, found by its place rather than by a ref per cell. */
function focusCell(grid: HTMLTableElement | null, at: MatrixPos): void {
  grid?.querySelector<HTMLElement>(`[data-row="${at.row}"][data-col="${at.col}"]`)?.focus();
}

/**
 * The whole tailnet at once: a row per source, a column per destination, a filled cell wherever the
 * policy opens the pair. What it opens is one hover away, since a cell has no room to say it.
 *
 * The grid is one tab stop: the arrows move the focus between cells and Home and End go to the ends
 * of the row, so the keyboard never has to walk through thousands of open pairs to leave it.
 */
export function AccessMatrix({
  matrix,
  onPick,
}: {
  readonly matrix: Matrix;
  /** Picking a machine from either axis opens its two lists. */
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  const grid = useRef<HTMLTableElement>(null);
  const [focused, setFocused] = useState<MatrixPos>({ row: 0, col: 0 });
  const size = matrix.columns.length;
  const at = clampCell(focused, size);
  const meta = matrix.open === 1 ? "1 pair open" : `${matrix.open} pairs open`;

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
      description={`Rows reach columns. ${meta}.`}
      bodyClassName="max-h-[36rem] overflow-auto p-0"
    >
      <table ref={grid} className={tableClass}>
        <thead>
          <tr>
            <th
              scope="col"
              className={cn(headerCellClass, rowHeaderClass, "z-20 border-b border-kumo-line")}
            >
              <span className="sr-only">Source machine</span>
            </th>
            {matrix.columns.map((node) => (
              <ColumnHeader key={node.id} node={node} onPick={onPick} />
            ))}
          </tr>
        </thead>
        <tbody>
          {matrix.rows.map((row, rowIndex) => (
            <tr key={row.src.id}>
              <th scope="row" className={rowHeaderClass}>
                <MachineButton
                  node={row.src}
                  onPick={onPick}
                  className="h-8 w-40 justify-start truncate px-0 text-base"
                />
              </th>
              {row.cells.map((cell, colIndex) => (
                <td key={cell.dst.id} className={cellClass}>
                  <Cell
                    cell={cell}
                    src={row.src}
                    at={{ row: rowIndex, col: colIndex }}
                    focused={at.row === rowIndex && at.col === colIndex}
                    onKeyDown={onKeyDown}
                  />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Section>
  );
}

function ColumnHeader({
  node,
  onPick,
}: {
  readonly node: AccessGraphNode;
  readonly onPick: (nodeId: string) => void;
}): ReactElement {
  return (
    <th scope="col" className={cn(headerCellClass, "border-b border-kumo-line")}>
      <MachineButton
        node={node}
        onPick={onPick}
        // Turned on its side so 60 machines fit across a column each, with the name reading up.
        className="h-40 w-6 rotate-180 justify-start truncate px-0 py-2 text-base [writing-mode:vertical-rl]"
      />
    </th>
  );
}

function MachineButton({
  node,
  onPick,
  className,
}: {
  readonly node: AccessGraphNode;
  readonly onPick: (nodeId: string) => void;
  readonly className: string;
}): ReactElement {
  return (
    <Button
      variant="ghost"
      size="sm"
      title={`${node.name} · ${ownerLabel(node)}`}
      className={cn("rounded-sm font-normal hover:text-kumo-link hover:underline", className)}
      onClick={() => {
        onPick(node.id);
      }}
    >
      {node.name}
    </Button>
  );
}

interface CellProps {
  readonly cell: MatrixCell;
  readonly src: AccessGraphNode;
  readonly at: MatrixPos;
  /** Whether this is the cell the grid's single tab stop currently sits on. */
  readonly focused: boolean;
  readonly onKeyDown: (event: KeyboardEvent, from: MatrixPos) => void;
}

/**
 * One pair. Every cell is a button, open or not, so the arrows walk a whole grid rather than
 * skipping the gaps; only the focused one is in the tab order.
 */
function Cell({ cell, src, at, focused, onKeyDown }: CellProps): ReactElement {
  const shared = {
    "data-row": at.row,
    "data-col": at.col,
    tabIndex: focused ? 0 : -1,
    onKeyDown: (event: KeyboardEvent): void => {
      onKeyDown(event, at);
    },
  };

  if (cell.self || cell.edge === undefined) {
    return (
      <button
        type="button"
        {...shared}
        aria-label={
          cell.self ? `${src.name}, its own row` : `${src.name} does not reach ${cell.dst.name}`
        }
        className={cn(squareClass, cell.self && "bg-kumo-recessed text-kumo-inactive")}
      >
        {cell.self ? <span aria-hidden>·</span> : null}
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
        aria-label={`${src.name} reaches ${cell.dst.name}: ${summary}`}
        className={cn(squareClass, "cursor-pointer")}
      >
        <span aria-hidden className="block size-3 rounded-xs bg-kumo-info" />
      </Popover.Trigger>
      <Popover.Content side="top" className="max-w-72 gap-1 p-3">
        <Popover.Title className="text-sm leading-5 font-medium">
          {src.name} → {cell.dst.name}
        </Popover.Title>
        <p className="text-sm text-kumo-subtle">{summary}</p>
      </Popover.Content>
    </Popover>
  );
}
