import { Breadcrumbs } from "@cloudflare/kumo/components/breadcrumbs";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { cn } from "@cloudflare/kumo/utils";
import { MagnifyingGlassIcon, SignOutIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { accessRequestsQuery, nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { can, displayName, roleLabel } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import { pendingCount } from "~/components/access/request-model.ts";
import { Mark } from "~/components/layout/mark.tsx";
import type { NavBadge, NavItem, NavPath, NavPlace } from "~/components/layout/nav.ts";
import { isActive, pagesOf, placeOf, visibleGroups } from "~/components/layout/nav.ts";
import { QuickSearch } from "~/components/layout/quick-search.tsx";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { BreadcrumbProvider, useBreadcrumbLeaf } from "~/lib/breadcrumbs.tsx";
import { pendingRouteCount } from "~/lib/node.ts";

/**
 * Every page gets the same content width, so navigating never moves the first column sideways.
 * Cards and tables fill it; the forms inside them keep their own narrower measures.
 */
const contentWidthClass = "max-w-[1400px]";

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
        <Sidebar className="md:sticky md:top-0 md:h-[calc(100svh-1px)] md:[&>div]:h-svh">
          <Sidebar.Header className="h-12">
            <Brand />
            {/* The drawer has no chrome of its own, so it carries the way out. */}
            <Sidebar.Close className="md:hidden" />
          </Sidebar.Header>
          <Sidebar.Content>
            <Sidebar.Group className="pb-4">
              <Sidebar.Menu>
                <Sidebar.MenuButton
                  icon={MagnifyingGlassIcon}
                  tooltip="Quick search (⌘K)"
                  className={cn(
                    touchRowClass,
                    "bg-kumo-base font-normal text-kumo-subtle ring ring-kumo-line group-data-[state=collapsed]/sidebar:bg-transparent group-data-[state=collapsed]/sidebar:ring-transparent",
                  )}
                  onClick={() => {
                    setSearchOpen(true);
                  }}
                >
                  <span className="flex flex-1 items-center justify-between gap-2">
                    Quick search…
                    <kbd className="rounded border border-kumo-hairline px-1 font-sans text-[10px] text-kumo-subtle">
                      ⌘K
                    </kbd>
                  </span>
                </Sidebar.MenuButton>
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
                        icon={item.icon}
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
        <div className="flex min-h-svh min-w-0 flex-1 flex-col bg-kumo-canvas">
          <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-4 lg:px-6">
            <Trail place={place} />
            <div className="flex items-center gap-2">
              <ThemeToggle />
              <AccountMenu me={me} />
            </div>
          </header>
          <main className="flex-1 px-4 py-6 sm:px-6 lg:px-10 lg:py-8">
            <div className={cn("mx-auto flex w-full flex-col gap-6", contentWidthClass)}>
              {children}
            </div>
          </main>
        </div>
        <QuickSearch me={me} pages={pages} open={searchOpen} onOpenChange={setSearchOpen} />
      </Sidebar.Provider>
    </BreadcrumbProvider>
  );
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
              icon={item.icon}
              active={active && !shown}
              tooltip={item.label}
              className={touchRowClass}
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
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
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

function CountBadge({ count }: { readonly count: number }): ReactElement | null {
  return count === 0 ? null : (
    <Sidebar.MenuBadge title={`${count} waiting`}>{count}</Sidebar.MenuBadge>
  );
}

function Brand(): ReactElement {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2 px-2 group-data-[state=collapsed]/sidebar:justify-center group-data-[state=collapsed]/sidebar:px-0">
      <Mark className="size-5 shrink-0 text-kumo-brand" />
      <span className="flex-1 truncate font-semibold text-kumo-strong group-data-[state=collapsed]/sidebar:hidden">
        slopscale
      </span>
    </div>
  );
}

/**
 * Breadcrumb trail: the sidebar item, then the page under it when the item is a branch, or the page
 * a detail route announced through useBreadcrumb. A branch's crumb goes straight to its first page
 * the caller may see, the one its own address would redirect to.
 */
function Trail({ place }: { readonly place: NavPlace | undefined }): ReactElement {
  const leaf = useBreadcrumbLeaf();
  const current = place?.item;
  const last: string | null = place?.child?.label ?? leaf;

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Sidebar.Trigger className="md:hidden" aria-label="Open navigation" />
      <Breadcrumbs>
        {current === undefined || last === null ? (
          <Breadcrumbs.Current>{current?.label ?? leaf ?? "slopscale"}</Breadcrumbs.Current>
        ) : (
          <>
            <Breadcrumbs.Link href={current.children?.[0]?.to ?? current.to}>
              {current.label}
            </Breadcrumbs.Link>
            <Breadcrumbs.Separator />
            <Breadcrumbs.Current>{last}</Breadcrumbs.Current>
          </>
        )}
      </Breadcrumbs>
    </div>
  );
}

const kindLabels: Record<string, string> = {
  local: "Local socket",
  api_key: "API key",
  oauth: "OAuth token",
  session: "Browser session",
};

function AccountMenu({ me }: { readonly me: Me }): ReactElement {
  const name = displayName(me);
  const role = roleLabel(me);

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <button
            type="button"
            aria-label="Account"
            title={name}
            className="flex cursor-pointer items-center rounded-md outline-none focus-visible:ring-2 focus-visible:ring-kumo-brand"
          >
            <Avatar name={name} size="lg" />
          </button>
        }
      />
      <DropdownMenu.Content align="end" className="min-w-56">
        <DropdownMenu.Group>
          <DropdownMenu.Label className="flex items-center gap-2">
            <Avatar name={name} size="lg" />
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate font-medium text-kumo-default">{name}</span>
              <span className="truncate text-xs text-kumo-subtle">
                {kindLabels[me.kind] ?? me.kind}
                {role === null ? "" : ` · ${role}`}
              </span>
            </span>
          </DropdownMenu.Label>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={SignOutIcon}
            onClick={() => {
              void signOut();
            }}
          >
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
