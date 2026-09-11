import { Empty } from "@cloudflare/kumo/components/empty";
import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { tableEmptyClass } from "~/components/table/empty.ts";
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
  footer,
  className,
  bodyClassName,
  panel = true,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly actions?: ReactNode;
  readonly children: ReactNode;
  /** A line on the band under the panel, such as a readout of what is under the pointer. */
  readonly footer?: ReactNode;
  readonly className?: string;
  /** Classes for the panel; pass `p-0` for lists that draw their own edges. */
  readonly bodyClassName?: string;
  /**
   * Whether the children go in a panel. A `frameTableClass` table draws the panel on its own body,
   * so it goes straight on the band, with its header as the band's last line.
   */
  readonly panel?: boolean;
}): ReactElement {
  return (
    <Frame className={className}>
      {/* px-5 puts the title where the text of the rows inside the panel starts. */}
      <FrameBand className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 px-5 pt-1.5 pb-2">
        <div className="flex min-w-0 flex-1 basis-56 flex-col gap-0.5">
          <h2 className="font-semibold text-kumo-strong">{title}</h2>
          {description === undefined ? null : (
            <p className="max-w-prose text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : (
          // min-h-lh keeps the actions on the title's first line, not centred on a long description.
          <div className="flex min-h-lh shrink-0 items-center gap-2">{actions}</div>
        )}
      </FrameBand>
      {panel ? <FramePanel className={bodyClassName}>{children}</FramePanel> : children}
      {footer === undefined ? null : <FrameBand className="px-5 pt-2 pb-1.5">{footer}</FrameBand>}
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

export interface SectionEmptyProps {
  readonly title: string;
  readonly description?: string;
  /** A way out of the empty state, such as clearing a search. */
  readonly contents?: ReactNode;
}

/**
 * The empty state of a Section panel. It is Kumo's Empty at the size a card uses, so a sub-panel
 * with nothing in it reads like the rows it stands in for rather than as a stray sentence.
 */
export function SectionEmpty({ title, description, contents }: SectionEmptyProps): ReactElement {
  return (
    <Empty
      className={tableEmptyClass}
      size="sm"
      title={title}
      {...(description === undefined ? {} : { description })}
      {...(contents === undefined ? {} : { contents })}
    />
  );
}
