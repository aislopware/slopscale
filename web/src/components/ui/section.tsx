import { cn } from "@cloudflare/kumo/utils";
import type { ReactElement, ReactNode } from "react";

import { cardClass } from "~/components/ui/card.tsx";

/**
 * The grey band of Kumo's layered card (the "white card resting on a grey tray"): rounded 8px, the
 * elevated fill, a hairline ring. It holds `TrayBand` rows and one or more `TrayPanel`s; `Section`
 * is the titled composition most places want.
 */
export function Tray({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return (
    <div
      className={cn(
        "flex min-w-0 flex-col rounded-lg bg-kumo-elevated ring ring-kumo-hairline",
        className,
      )}
    >
      {children}
    </div>
  );
}

/** A row of text or small controls on a Tray's band, 16px in, 8px above and below. */
export function TrayBand({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return (
    <div className={cn("flex min-h-10 items-center gap-2 px-4 py-2", className)}>{children}</div>
  );
}

/**
 * The white panel resting in a Tray, flush with its sides at the same radius. It clips with
 * overflow-clip, so the corners stay round without the panel becoming a scroll container.
 */
export function TrayPanel({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string | undefined;
}): ReactElement {
  return <div className={cn("min-w-0 overflow-clip", cardClass, className)}>{children}</div>;
}

/**
 * Kumo's layered card as the Cloudflare dashboard uses it for grouped content (Security rules,
 * Connected Workers, the API card): a grey elevated band, ringed by a hairline, carrying the title
 * (13px semibold), its meta (12px subtle, "4/20 rules") and small actions; a white panel resting in
 * it with the content; and an optional footer band under the panel for a link out. The panel runs
 * flush to the band's sides at the same 8px radius, so the two rings meet at the corners.
 */
export function Section({
  title,
  meta,
  description,
  actions,
  children,
  footer,
  className,
  bodyClassName,
  panel = true,
}: {
  readonly title: ReactNode;
  /** Small facts beside the title: a count, a limit, a state. */
  readonly meta?: ReactNode;
  /** A sentence under the title, on the band. */
  readonly description?: ReactNode;
  /** Small (`size="sm"`) secondary buttons on the band's right. */
  readonly actions?: ReactNode;
  readonly children: ReactNode;
  /** A line on the band under the panel, such as a "Go to …" link button. */
  readonly footer?: ReactNode;
  readonly className?: string;
  /** Classes for the panel; pass `p-0` for lists that draw their own edges. */
  readonly bodyClassName?: string;
  /**
   * Whether the children go in the white panel, or straight on the band (a table with its own
   * frame).
   */
  readonly panel?: boolean;
}): ReactElement {
  return (
    <section
      className={cn(
        "flex min-w-0 flex-col rounded-lg bg-kumo-elevated ring ring-kumo-hairline",
        className,
      )}
    >
      <div className="flex min-h-11 flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2">
        <div className="flex min-w-0 flex-1 basis-56 flex-col gap-0.5">
          <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
            <h2 className="text-sm font-semibold text-kumo-default">{title}</h2>
            {meta === undefined ? null : <span className="text-xs text-kumo-subtle">{meta}</span>}
          </div>
          {description === undefined ? null : (
            <p className="max-w-[750px] text-sm text-pretty text-kumo-subtle">{description}</p>
          )}
        </div>
        {actions === undefined ? null : (
          <div className="flex shrink-0 items-center gap-2">{actions}</div>
        )}
      </div>
      {panel ? <TrayPanel className={bodyClassName}>{children}</TrayPanel> : children}
      {footer === undefined ? null : (
        <div className="flex items-center justify-end gap-2 px-4 py-2">{footer}</div>
      )}
    </section>
  );
}

/** A row inside a Section panel, divided from the one above by a line. */
export function SectionRow({
  className,
  children,
}: {
  readonly className?: string;
  readonly children: ReactNode;
}): ReactElement {
  return (
    <div className={cn("px-4 py-3 not-first:border-t not-first:border-kumo-line", className)}>
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
 * The empty state inside a panel, as the dashboard writes it: a centred line of subtle text with
 * 48px above and below ("No queues matched your search. Try searching for a different name."), and
 * the way out under it.
 */
export function SectionEmpty({ title, description, contents }: SectionEmptyProps): ReactElement {
  return (
    <div className="flex flex-col items-center gap-3 px-4 py-12 text-center">
      <div className="flex max-w-[60ch] flex-col text-pretty text-kumo-subtle">
        <h3 className="font-normal text-kumo-default">{title}</h3>
        {description === undefined ? null : <p>{description}</p>}
      </div>
      {contents === undefined ? null : <div className="flex items-center gap-2">{contents}</div>}
    </div>
  );
}
