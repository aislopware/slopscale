import { cn } from "@cloudflare/kumo/utils";
import { ArrowSquareOutIcon } from "@phosphor-icons/react";
import type { ReactElement, ReactNode } from "react";

import { AppLink } from "~/lib/link.tsx";

/**
 * The Cloudflare dashboard's page grid: a 1200px column centred between two gutters of at least
 * 16px (32px from md), 32px above and below, 28px between blocks. Every child sits in the centre
 * column; a child marked `data-full-bleed` spans the gutters too, edge to edge, which is how the
 * rule under a page header and a branch's sub-navigation bar reach from the sidebar to the
 * viewport. The shell lays every page out on it, so a route returns its blocks as a fragment.
 */
export const pageGridClass = cn(
  "grid content-start gap-y-7 py-8",
  "[--page-max-width:1200px] [--page-padding-x:16px] md:[--page-padding-x:32px]",
  "grid-cols-[minmax(var(--page-padding-x),1fr)_minmax(0,var(--page-max-width))_minmax(var(--page-padding-x),1fr)]",
  "*:col-start-2 *:min-w-0 *:data-full-bleed:col-span-full",
  // A branch's sub-navigation bar (53px) sits under the 58px top bar; whatever sticks inside the
  // page comes to rest under both.
  "has-[>[data-sub-nav]]:[--app-header-height:111px]",
);

/** A block that spans the page edge to edge instead of the centre column. */
export function FullBleed({
  children,
  className,
}: {
  readonly children?: ReactNode;
  readonly className?: string;
}): ReactElement {
  return (
    <div data-full-bleed className={className}>
      {children}
    </div>
  );
}

/**
 * A page with a 400px rail on its right, as the dashboard's zone overview and Workers list have:
 * the page's own blocks on a page grid of their own on the left, the rail on the right behind a
 * line, its content sticky under the top bar. Below the `@5xl` container width (1024px of content)
 * the rail drops under the page. It takes the whole page, so a route renders it as its only block,
 * with its `PageHeader` inside.
 */
export function PageWithRail({
  children,
  rail,
}: {
  readonly children: ReactNode;
  /** The rail's sections, usually `RailSection`s; 20px apart. */
  readonly rail: ReactNode;
}): ReactElement {
  return (
    // -my-8 takes back the shell grid's own padding, so the rail's line runs from the top bar down.
    <div
      data-full-bleed
      className="-my-8 grid [--page-sidebar-width:400px] @5xl:grid-cols-[minmax(0,1fr)_var(--page-sidebar-width)]"
    >
      <div className={pageGridClass}>{children}</div>
      <aside className="border-t border-kumo-line @5xl:border-t-0 @5xl:border-l">
        <div className="flex flex-col gap-5 px-4 py-8 md:px-8 @5xl:sticky @5xl:top-(--app-header-height,0px)">
          {rail}
        </div>
      </aside>
    </div>
  );
}

/** A titled part of a rail: a 16px semibold heading (24px for the rail's lead figure) over its rows. */
export function RailSection({
  title,
  size = "base",
  actions,
  children,
}: {
  readonly title: ReactNode;
  readonly size?: "base" | "lg";
  readonly actions?: ReactNode;
  readonly children?: ReactNode;
}): ReactElement {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <h2
          className={cn("font-semibold text-kumo-default", size === "lg" ? "text-2xl" : "text-lg")}
        >
          {title}
        </h2>
        {actions === undefined ? null : <div className="flex items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

/** The rows of a rail section, divided by lines. */
export function RailList({ children }: { readonly children: ReactNode }): ReactElement {
  return (
    <div className="flex flex-col divide-y divide-kumo-line border-y border-kumo-line">
      {children}
    </div>
  );
}

const railLinkClass =
  "inline-flex w-fit items-center gap-1 text-xs text-kumo-link hover:text-kumo-link/70";

/**
 * A rail row's link: a console page through the router, which adds the /console/ base path and
 * navigates without a reload; anything else in a new tab with the external icon.
 */
function RailLink({
  href,
  label,
  external = false,
}: {
  readonly href: string;
  readonly label: string;
  readonly external?: boolean;
}): ReactElement {
  if (!external) {
    return (
      <AppLink href={href} className={railLinkClass}>
        {label}
      </AppLink>
    );
  }

  return (
    <a href={href} target="_blank" rel="noreferrer" className={railLinkClass}>
      {label}
      <ArrowSquareOutIcon aria-hidden className="size-3" />
    </a>
  );
}

/**
 * One row of a rail: a 13px medium title, a 12px subtle description, an optional 12px link (with
 * the external icon when it leaves the console) and a control such as a Switch on the right.
 */
export function RailRow({
  title,
  description,
  link,
  control,
}: {
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly link?: {
    readonly href: string;
    readonly label: string;
    readonly external?: boolean;
  };
  readonly control?: ReactNode;
}): ReactElement {
  return (
    <div className="flex items-start justify-between gap-4 py-3">
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="text-sm font-medium text-kumo-default">{title}</span>
        {description === undefined ? null : (
          <span className="text-xs text-kumo-subtle">{description}</span>
        )}
        {link === undefined ? null : <RailLink {...link} />}
      </div>
      {control === undefined ? null : <div className="flex shrink-0 items-center">{control}</div>}
    </div>
  );
}
