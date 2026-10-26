import type { ReactElement, ReactNode } from "react";

import { FullBleed } from "~/components/ui/page.tsx";

/**
 * The title block of every page, in the Cloudflare dashboard's current style: a 20px semibold title
 * with an optional 12px kicker above it, a subtle description kept to 750px, an optional meta line
 * of small facts, and the page's actions on the right (h-9, secondary before primary). Under it a
 * rule runs the full width of the page, edge to edge. It renders two blocks of the page grid, the
 * header and the rule, so it goes straight in the route's fragment rather than inside a wrapper.
 */
export function PageHeader({
  title,
  kicker,
  description,
  meta,
  actions,
}: {
  readonly title: ReactNode;
  /** A 12px word above the title naming the part of a thing this page is: "Overview". */
  readonly kicker?: ReactNode;
  readonly description?: ReactNode;
  /** Small muted facts under the description, e.g. "3 machines · 2 connected". */
  readonly meta?: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <>
      <header className="flex flex-wrap items-center justify-between gap-x-6 gap-y-4">
        <div className="flex min-w-0 flex-col gap-2">
          <div className="flex min-w-0 flex-col gap-1">
            {kicker === undefined ? null : <p className="text-xs text-kumo-subtle">{kicker}</p>}
            <div className="flex min-w-0 items-center gap-2">
              <h1 className="truncate text-[20px] leading-[25px] font-semibold text-kumo-default">
                {title}
              </h1>
            </div>
          </div>
          {description === undefined ? null : (
            <p className="max-w-[750px] text-pretty text-kumo-subtle">{description}</p>
          )}
          {meta === undefined ? null : (
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-kumo-subtle">
              {meta}
            </div>
          )}
        </div>
        {actions === undefined ? null : (
          // Wraps rather than shrinks: a row of three controls is wider than a phone, and squeezing
          // them would cut their labels instead of stacking.
          <div className="flex max-w-full shrink-0 flex-wrap items-center gap-2">{actions}</div>
        )}
      </header>
      <FullBleed className="border-b border-kumo-line" />
    </>
  );
}
