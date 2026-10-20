import { LinkButton } from "@cloudflare/kumo/components/button";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import { BookOpenTextIcon } from "@phosphor-icons/react";
import { Outlet, useRouteContext, useRouterState } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Me } from "~/auth/me.ts";
import type { NavChild, NavItem, NavPath } from "~/components/layout/nav.ts";
import { visibleGroups } from "~/components/layout/nav.ts";
import { docsUrl } from "~/lib/docs.ts";
import { AppLink } from "~/lib/link.tsx";

/** The branch of the sidebar at `area`, as far as the caller may see it. */
function branchOf(me: Me, area: NavPath): NavItem | undefined {
  return visibleGroups(me)
    .flatMap((group) => group.items)
    .find((item) => item.to === area);
}

/** The page of the branch the address is, when it is one of the branch's own pages. */
function pageOf(children: readonly NavChild[], pathname: string): NavChild | undefined {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/u, "") : pathname;

  return children.find((child) => child.to === path);
}

/**
 * The Zero Trust sub-navigation bar (design spec §13.2, §13.15.2): a sticky 53px bar under the top
 * bar with the branch's pages as a 36px segmented Kumo `Tabs` track, each tab a link to its own
 * address, and the branch's documentation on the right. It shows on the branch's own pages only; a
 * detail or form page under one (a rule, a new key) has its breadcrumbs and its own header instead.
 * The sidebar keeps its sub-menus, so the bar is a second way, not the only one.
 *
 * It spans the page edge to edge and lines its tabs up with the page's column. While it is on the
 * page, the page grid raises `--app-header-height` by its height (see `pageGridClass`), so the
 * sticky toolbars, rails and banners inside the page come to rest under it, not behind it.
 */
export function AreaSubNav({
  me,
  area,
}: {
  readonly me: Me;
  readonly area: NavPath;
}): ReactElement | null {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const branch = branchOf(me, area);
  const children = branch?.children ?? [];
  const current = pageOf(children, pathname);

  if (branch === undefined || current === undefined || children.length < 2) {
    return null;
  }

  return (
    <nav
      data-full-bleed
      data-sub-nav
      aria-label={branch.label}
      className="sticky top-(--app-bar-height,0px) z-10 -mt-8 border-b border-kumo-line bg-kumo-canvas"
    >
      <div className="grid grid-cols-[minmax(var(--page-padding-x),1fr)_minmax(0,var(--page-max-width))_minmax(var(--page-padding-x),1fr)]">
        <div className="col-start-2 flex h-[52px] min-w-0 items-center justify-between gap-3">
          <Tabs
            variant="segmented"
            className="min-w-0"
            value={current.to}
            tabs={children.map((child) => ({
              value: child.to,
              label: child.label,
              render: <AppLink href={child.to} />,
              nativeButton: false,
            }))}
          />
          {branch.docs === undefined ? null : (
            <LinkButton
              variant="secondary"
              href={docsUrl(branch.docs)}
              external
              icon={<BookOpenTextIcon aria-hidden />}
              className="shrink-0 max-md:hidden"
            >
              {`${branch.label} documentation`}
            </LinkButton>
          )}
        </div>
      </div>
    </nav>
  );
}

/**
 * The layout of a sidebar branch's route: its sub-navigation bar over the page. A branch's
 * `route.tsx` renders this, so every page under it gets the bar without asking for it.
 */
export function AreaLayout({ area }: { readonly area: NavPath }): ReactElement {
  const { me } = useRouteContext({ from: "/_app" });

  return (
    <>
      <AreaSubNav me={me} area={area} />
      <Outlet />
    </>
  );
}
