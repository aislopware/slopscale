import { Outlet, useRouteContext, useRouterState } from "@tanstack/react-router";
import type { ReactElement } from "react";

import type { Me } from "~/auth/me.ts";
import type { NavChild, NavItem, NavPath } from "~/components/layout/nav.ts";
import { visibleGroups } from "~/components/layout/nav.ts";
import { TabBar } from "~/components/layout/tab-bar.tsx";

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
 * A sidebar branch's sub-navigation: the `TabBar` with the branch's pages as its tabs and the
 * branch's documentation on the right. It shows on the branch's own pages only; a detail or form
 * page under one (a rule, a new key) has its breadcrumbs and its own header instead. The sidebar
 * keeps its sub-menus, so the bar is a second way, not the only one.
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
    <TabBar
      label={branch.label}
      value={current.to}
      tabs={children.map((child) => ({ value: child.to, label: child.label, href: child.to }))}
      {...(branch.docs === undefined
        ? {}
        : { docs: { path: branch.docs, label: `${branch.label} documentation` } })}
    />
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
