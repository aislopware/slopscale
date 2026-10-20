import { Breadcrumbs } from "@cloudflare/kumo/components/breadcrumbs";
import { LinkButton } from "@cloudflare/kumo/components/button";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import { MagnifyingGlassIcon, QuestionIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { accessRequestsQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { can, canSeeMachines } from "~/auth/me.ts";
import { pendingCount } from "~/components/access/request-model.ts";
import { AccountMenu } from "~/components/layout/account-menu.tsx";
import { BrandMark, useBrand } from "~/components/layout/brand.tsx";
import type { NavBadge, NavItem, NavPath, NavPlace } from "~/components/layout/nav.ts";
import { isActive, pagesOf, placeOf, visibleGroups } from "~/components/layout/nav.ts";
import { QuickSearch } from "~/components/layout/quick-search.tsx";
import { pageGridClass } from "~/components/ui/page.tsx";
import { BreadcrumbProvider, useBreadcrumbLeaf } from "~/lib/breadcrumbs.tsx";
import { docsBase } from "~/lib/docs.ts";
import { pendingRouteCount } from "~/lib/node.ts";

/**
 * The sidebar sits on the page's canvas, divided from it by one line, as the Cloudflare dashboard's
 * does; hover, the current page and an open branch share the recessed fill (the control fill in
 * dark mode, where recessed is darker than the canvas).
 */
const sidebarSurfaceClass =
  "[--sidebar-bg:var(--color-kumo-canvas)] [--sidebar-active-bg:light-dark(var(--color-kumo-recessed),var(--color-kumo-control))]";

/** A nav icon at half strength, so the 13px labels lead and the icons only mark the rows. */
function navIcon(NavIcon: Icon): ReactElement {
  return <NavIcon aria-hidden className="size-4 shrink-0 opacity-50" />;
}

/**
 * The drawer is reached with a thumb, so its rows meet the 44px touch target. The desktop rail
 * keeps its compact rows, which are aimed with a pointer.
 */
const touchRowClass = "max-md:min-h-11";

export function Shell({
  me,
  children,
}: {
  readonly me: Me;
  readonly children: ReactNode;
}): ReactElement {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const groups = visibleGroups(me);
  const pages = pagesOf(groups);
  const place = placeOf(groups, pathname);
  const counts = usePendingCounts(me);
  const [searchOpen, setSearchOpen] = useState(false);
  // What the reader clicked open or shut, and on which page. Arriving at another page forgets it,
  // so the branch the page is under opens and the others settle closed: the sidebar shows where
  // the reader is, not where they poked.
  const [toggles, setToggles] = useState<Toggles>({ at: pathname, open: {} });
  const toggled = toggles.at === pathname ? toggles.open : {};

  return (
    <BreadcrumbProvider>
      <Sidebar.Provider defaultOpen collapsible="icon" peekable>
        {/*
         * A sticky element may not pass the bottom of its parent, and the page's height is
         * fractional while its scroll height is rounded up, so at the very bottom of a long page
         * the sidebar was dragged up by that fraction and every row in it shifted. A rail one
         * pixel short of the viewport always has the slack to stay put; its own panel keeps the
         * full height, so nothing shows through under it.
         */}
        <Sidebar
          className={cn(
            sidebarSurfaceClass,
            "md:sticky md:top-0 md:h-[calc(100svh-1px)] md:[&>div]:h-svh",
          )}
        >
          <Sidebar.Header>
            <Brand />
            {/* The drawer has no chrome of its own, so it carries the way out. */}
            <Sidebar.Close className="md:hidden" />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group className="pb-1">
              <Sidebar.Menu>
                <QuickSearchButton
                  onOpen={() => {
                    setSearchOpen(true);
                  }}
                />
              </Sidebar.Menu>
            </Sidebar.Group>
            {groups.map((group, index) => (
              <Sidebar.Group key={group.label ?? index}>
                {group.label === undefined ? null : (
                  <Sidebar.GroupLabel>{group.label}</Sidebar.GroupLabel>
                )}
                <Sidebar.Menu>
                  {group.items.map((item) =>
                    item.children === undefined ? (
                      <Sidebar.MenuButton
                        key={item.to}
                        href={item.to}
                        icon={navIcon(item.icon)}
                        active={isActive(item, pathname)}
                        tooltip={item.label}
                        className={touchRowClass}
                      >
                        {item.label}
                        <PendingBadge badge={item.badge} counts={counts} />
                      </Sidebar.MenuButton>
                    ) : (
                      <NavBranch
                        key={item.to}
                        item={item}
                        pathname={pathname}
                        counts={counts}
                        open={toggled[item.to]}
                        onOpenChange={(open) => {
                          setToggles({ at: pathname, open: { ...toggled, [item.to]: open } });
                        }}
                      />
                    ),
                  )}
                </Sidebar.Menu>
              </Sidebar.Group>
            ))}
          </Sidebar.Content>
          {/* Collapsing to the icon rail is a desktop idea; the drawer is either open or gone. The
              trigger sits at the left edge, in the column the icons are in, so it is in the same
              place on the rail and on the open sidebar. */}
          <Sidebar.Footer className="justify-start max-md:hidden">
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <div className="flex min-h-svh min-w-0 flex-1 flex-col bg-kumo-canvas [--app-header-height:58px]">
          {/* 58px like the sidebar's header, so the bottom borders run as one line; sticky toolbars
              and rails come to rest under it through the variable. */}
          <header className="sticky top-0 z-10 flex h-(--app-header-height) shrink-0 items-center justify-between gap-2 border-b border-kumo-line bg-kumo-canvas px-4">
            <RouteProgress />
            <Trail place={place} pathname={pathname} />
            <div className="flex shrink-0 items-center gap-1">
              <LinkButton
                variant="ghost"
                href={docsBase}
                external
                icon={
                  <QuestionIcon aria-hidden weight="fill" className="size-4 text-kumo-subtle" />
                }
                className="h-8 px-3 font-medium max-sm:hidden"
              >
                Docs
              </LinkButton>
              <AccountMenu me={me} />
            </div>
          </header>
          <main className="@container flex-1">
            <div className={pageGridClass}>{children}</div>
          </main>
        </div>
        <QuickSearch me={me} pages={pages} open={searchOpen} onOpenChange={setSearchOpen} />
        <CurrentPageIntoView key={pathname} />
      </Sidebar.Provider>
    </BreadcrumbProvider>
  );
}

/**
 * The quick search at the top of the sidebar, the dashboard's: a 32px secondary-looking button with
 * the ⌘K hint at its end. On the icon rail it is the magnifier alone.
 */
function QuickSearchButton({ onOpen }: { readonly onOpen: () => void }): ReactElement {
  return (
    <Sidebar.MenuButton
      icon={navIcon(MagnifyingGlassIcon)}
      tooltip="Quick search (⌘K)"
      className={cn(
        touchRowClass,
        "min-h-8 bg-kumo-base font-normal text-kumo-subtle shadow-xs ring ring-kumo-line hover:bg-kumo-base",
        "group-data-[state=collapsed]/sidebar:bg-transparent group-data-[state=collapsed]/sidebar:shadow-none group-data-[state=collapsed]/sidebar:ring-transparent",
      )}
      onClick={onOpen}
    >
      <span className="flex flex-1 items-center justify-between gap-2">
        Quick search…
        <kbd className="font-sans text-xs opacity-50">⌘K</kbd>
      </span>
    </Sidebar.MenuButton>
  );
}

/**
 * On a short screen the pages low in the sidebar sit below its fold, so the current one would be
 * out of sight. Keyed by the path, this brings it into view on every page change, a frame later so
 * its branch has opened.
 */
function CurrentPageIntoView(): null {
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      document
        .querySelector('[data-sidebar="content"] [data-active]')
        ?.scrollIntoView({ block: "nearest" });
    });

    return (): void => {
      cancelAnimationFrame(frame);
    };
  }, []);

  return null;
}

interface Toggles {
  /** The page the reader was on when they clicked. */
  readonly at: string;
  readonly open: Partial<Record<NavPath, boolean>>;
}

/**
 * A branch of the sidebar: a button that opens and closes the pages under it. It opens on its own
 * while one of them is the current page, and shows as current itself only while closed, so the
 * highlight is on one row at a time. Closed, it carries the sum of its pages' counts.
 */
function NavBranch({
  item,
  pathname,
  counts,
  open,
  onOpenChange,
}: {
  readonly item: NavItem;
  readonly pathname: string;
  readonly counts: PendingCounts;
  /** What the reader last clicked it to, or nothing since the last page change. */
  readonly open: boolean | undefined;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const children = item.children ?? [];
  const active = isActive(item, pathname);
  const shown = open ?? active;
  const total = children.reduce(
    (sum, child) => sum + (child.badge === undefined ? 0 : counts[child.badge]),
    0,
  );

  return (
    <Sidebar.MenuItem>
      <Sidebar.Collapsible open={shown} onOpenChange={onOpenChange}>
        <Sidebar.CollapsibleTrigger
          render={
            <Sidebar.MenuButton
              icon={navIcon(item.icon)}
              active={active && !shown}
              tooltip={item.label}
              // An open branch keeps the active fill, so the branch and its pages read as one block.
              className={cn(touchRowClass, shown && "bg-(--sidebar-active-bg)")}
            >
              {item.label}
              {shown ? null : <CountBadge count={total} />}
              <Sidebar.MenuChevron />
            </Sidebar.MenuButton>
          }
        />
        <Sidebar.CollapsibleContent>
          <Sidebar.MenuSub>
            {children.map((child) => (
              <Sidebar.MenuSubButton
                key={child.to}
                href={child.to}
                active={isActive(child, pathname)}
                className={touchRowClass}
              >
                {child.label}
                <PendingBadge badge={child.badge} counts={counts} />
              </Sidebar.MenuSubButton>
            ))}
          </Sidebar.MenuSub>
        </Sidebar.CollapsibleContent>
      </Sidebar.Collapsible>
    </Sidebar.MenuItem>
  );
}

type PendingCounts = Readonly<Record<NavBadge, number>>;

function usePendingCounts(me: Me): PendingCounts {
  const nodes = useQuery({ ...nodesQuery, enabled: canSeeMachines(me) });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });
  const requests = useQuery({ ...accessRequestsQuery, enabled: can(me, "policy_file:read") });
  const nodeList = nodes.data?.nodes ?? [];

  return {
    pendingNodes: nodeList.filter((node) => !node.approved).length,
    pendingUsers: (users.data?.users ?? []).filter((user) => !user.approved).length,
    pendingRoutes: nodeList.reduce((sum, node) => sum + pendingRouteCount(node), 0),
    pendingRequests: pendingCount(requests.data?.requests ?? []),
  };
}

function PendingBadge({
  badge,
  counts,
}: {
  readonly badge: NavBadge | undefined;
  readonly counts: PendingCounts;
}): ReactElement | null {
  return badge === undefined ? null : <CountBadge count={counts[badge]} />;
}

/**
 * How many things wait for the reader, as Kumo's count dot: a brand-blue pill with the figure in
 * 10px, the one coloured mark in the sidebar, so it reads as something to act on.
 */
function CountBadge({ count }: { readonly count: number }): ReactElement | null {
  return count === 0 ? null : (
    <span
      title={`${count} waiting`}
      className="inline-flex h-4 min-w-4 shrink-0 items-center justify-center rounded-full bg-kumo-brand px-1 text-[10px] leading-none font-medium text-kumo-inverse tabular-nums group-data-[state=collapsed]/sidebar:hidden"
    >
      {count}
    </span>
  );
}

/**
 * The top of the sidebar: the mark and the tailnet's name, in the place and the shape of the
 * dashboard's account switcher. There is one tailnet, so it is a way home rather than a menu.
 */
function Brand(): ReactElement {
  const brand = useBrand();

  return (
    <Link
      to="/"
      className="flex min-w-0 flex-1 items-center gap-2.5 rounded-lg px-2 py-1.5 text-kumo-default no-underline outline-none group-data-[state=collapsed]/sidebar:justify-center group-data-[state=collapsed]/sidebar:px-0 hover:bg-kumo-tint focus-visible:ring-2 focus-visible:ring-kumo-focus"
    >
      <BrandMark markClassName="size-6 shrink-0" logoClassName="h-6 max-w-24 shrink-0" />
      <span className="flex-1 truncate text-sm font-medium group-data-[state=collapsed]/sidebar:hidden">
        {brand.title}
      </span>
    </Link>
  );
}

/** The tab's title from the trail: "backup-nas - Machines - Slopscale", so tabs and history read. */
export function documentTitle(
  brand: string,
  section: string | undefined,
  page: string | null,
): string {
  return [page, section, brand]
    .filter((part): part is string => part !== undefined && part !== null && part !== "")
    .filter((part, index, parts) => parts.indexOf(part) === index)
    .join(" - ");
}

function useDocumentTitle(title: string): void {
  useEffect(() => {
    document.title = title;
  }, [title]);
}

/**
 * Breadcrumb trail: the sidebar item, then the page under it when the item is a branch, or the page
 * a detail route announced through useBreadcrumb. A branch's crumb goes straight to its first page
 * the caller may see, the one its own address would redirect to. A detail page below a page of a
 * branch (one machine's traffic under Traffic, Machines) gets all three: branch, page, detail.
 */
function Trail({
  place,
  pathname,
}: {
  readonly place: NavPlace | undefined;
  readonly pathname: string;
}): ReactElement {
  const leaf = useBreadcrumbLeaf();
  const brand = useBrand();
  const current = place?.item;
  const page = place?.child;
  const detail = page !== undefined && pathname !== page.to && leaf !== null ? leaf : null;
  const last: string | null = page?.label ?? leaf;

  useDocumentTitle(documentTitle(brand.title, current?.label, detail ?? last));

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Sidebar.Trigger className="md:hidden" aria-label="Open navigation" />
      {/* A page with nothing above it has no trail: its title is on the page, and the dashboard
          leaves the top bar empty there too. */}
      {current === undefined || last === null ? null : (
        <Breadcrumbs>
          <Breadcrumbs.Link href={current.children?.[0]?.to ?? current.to}>
            {current.label}
          </Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          {detail === null || page === undefined ? (
            <Breadcrumbs.Current>{last}</Breadcrumbs.Current>
          ) : (
            <>
              <Breadcrumbs.Link href={page.to}>{page.label}</Breadcrumbs.Link>
              <Breadcrumbs.Separator />
              <Breadcrumbs.Current>{detail}</Breadcrumbs.Current>
            </>
          )}
        </Breadcrumbs>
      )}
    </div>
  );
}

/**
 * The line along the top of the header while the next page's loader runs. The router keeps the
 * current page on screen until the data is in, so without it a click on the sidebar does nothing
 * visible for as long as the request takes; with it the click has landed. It appears only after
 * 150ms, so a page whose data is already cached never flashes it, and goes at once.
 */
function RouteProgress(): ReactElement {
  const pending = useRouterState({ select: (state) => state.status === "pending" });

  return (
    <div
      aria-hidden
      className={cn(
        "pointer-events-none absolute inset-x-0 top-0 h-0.5 overflow-hidden transition-opacity duration-0",
        pending ? "opacity-100 delay-150" : "opacity-0 delay-0",
      )}
    >
      <div className="h-full w-1/3 animate-route-progress bg-kumo-brand" />
    </div>
  );
}
