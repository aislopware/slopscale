import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { Frame, FrameBand, FramePanel } from "~/components/ui/frame.tsx";

/**
 * A page section: a Frame whose band carries the 14px semibold title (with optional description and
 * actions) and whose panel holds the content. Panels never nest, so a page reads as a list of named
 * regions rather than a stack of identical boxes.
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
  /** Classes for the panel; pass `p-0` for tables and lists that draw their own edges. */
  readonly bodyClassName?: string;
}): ReactElement {
  return (
    <Frame className={className}>
      {/* px-5 puts the title where the text of the rows inside the panel starts. */}
      <FrameBand className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2 px-5 pt-1.5 pb-2">
        <div className="flex min-w-0 flex-1 basis-56 flex-col gap-0.5">
          <h2 className="font-semibold text-kumo-strong">{title}</h2>
          {description === undefined ? null : (
            <p className="max-w-prose text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </FrameBand>
      <FramePanel className={bodyClassName}>{children}</FramePanel>
    </Frame>
  );
}

/** A row inside a Section panel with the console's padding; hairlines between siblings. */
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
