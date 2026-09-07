import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

/**
 * A page section: a 14px semibold title (with optional description and actions) above a single
 * LayerCard surface. The title sits outside the card, so cards never nest and a page reads as a
 * list of named regions rather than a stack of identical boxes.
 */
export function Section({
  title,
  description,
  actions,
  children,
  className,
  bodyClassName,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly actions?: ReactNode;
  readonly children: ReactNode;
  readonly className?: string;
  /** Classes for the card; pass `p-0` for tables and lists that draw their own edges. */
  readonly bodyClassName?: string;
}): ReactElement {
  return (
    <section className={cn("flex flex-col gap-2", className)}>
      <header className="flex flex-wrap items-end justify-between gap-x-4 gap-y-1 px-0.5">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-semibold text-kumo-strong">{title}</h2>
          {description === undefined ? null : (
            <p className="text-xs text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : <div className="flex items-center gap-2">{actions}</div>}
      </header>
      <LayerCard className={cn("overflow-hidden", bodyClassName)}>{children}</LayerCard>
    </section>
  );
}

/** A row inside a Section card with the console's padding; hairlines between siblings. */
export function SectionRow({
  className,
  children,
}: {
  readonly className?: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className={cn("px-5 py-4 not-first:border-t not-first:border-kumo-hairline", className)}>
      {children}
    </div>
  );
}
