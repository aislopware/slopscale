import type { ReactElement, ReactNode } from "react";

/**
 * A titled table on a traffic page, the way Zero Trust heads the lists inside a page: a 16px
 * semibold heading with a subtle sentence under it and the table's own controls (a grouping, say)
 * on the right, then the table in its own card with its count and links under it. The table draws
 * its frame, so nothing wraps it in a second one.
 */
export function TableSection({
  title,
  description,
  actions,
  children,
}: {
  readonly title: string;
  readonly description?: string;
  readonly actions?: ReactNode;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <section className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          <h2 className="text-lg font-semibold text-kumo-strong">{title}</h2>
          {description === undefined ? null : (
            <p className="max-w-[750px] text-pretty text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      {children}
    </section>
  );
}
