import { Breadcrumbs } from "@cloudflare/kumo/components/breadcrumbs";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { cn } from "@cloudflare/kumo/utils";
import { MagnifyingGlassIcon, SignOutIcon, WaveformIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { nodesQuery, usersQuery } from "~/api/queries.ts";
import type { Me } from "~/auth/me.ts";
import { can, displayName, roleLabel } from "~/auth/me.ts";
import { signOut } from "~/auth/session.ts";
import type { NavItem } from "~/components/layout/nav.ts";
import { isActive, visibleGroups } from "~/components/layout/nav.ts";
import { QuickSearch } from "~/components/layout/quick-search.tsx";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";
import { Avatar } from "~/components/ui/avatar.tsx";
import { BreadcrumbProvider, useBreadcrumbLeaf } from "~/lib/breadcrumbs.tsx";

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
  const pages = groups.flatMap((group) => group.items);
  const current = pages.find((item) => isActive(item, pathname));
  const counts = usePendingCounts(me);
  const [searchOpen, setSearchOpen] = useState(false);

  return (
    <BreadcrumbProvider>
      <Sidebar.Provider defaultOpen collapsible="icon" peekable>
        <Sidebar className="md:sticky md:top-0 md:h-svh">
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
                  {group.items.map((item) => (
                    <Sidebar.MenuButton
                      key={item.to}
                      href={item.to}
                      icon={item.icon}
                      active={isActive(item, pathname)}
                      tooltip={item.label}
                      className={touchRowClass}
                    >
                      {item.label}
                      <PendingBadge item={item} counts={counts} />
                    </Sidebar.MenuButton>
                  ))}
                </Sidebar.Menu>
              </Sidebar.Group>
            ))}
          </Sidebar.Content>
          {/* Collapsing to the icon rail is a desktop idea; the drawer is either open or gone. */}
          <Sidebar.Footer className="justify-end max-md:hidden">
            <Sidebar.Trigger />
          </Sidebar.Footer>
        </Sidebar>
        <div className="flex min-h-svh min-w-0 flex-1 flex-col bg-kumo-canvas">
          <header className="sticky top-0 z-10 flex h-12 shrink-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-4 lg:px-6">
            <Trail current={current} />
            <div className="flex items-center gap-1">
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

interface PendingCounts {
  readonly pendingNodes: number;
  readonly pendingUsers: number;
}

function usePendingCounts(me: Me): PendingCounts {
  const nodes = useQuery({ ...nodesQuery, enabled: can(me, "devices:core:read") });
  const users = useQuery({ ...usersQuery, enabled: can(me, "users:read") });

  return {
    pendingNodes: (nodes.data?.nodes ?? []).filter((node) => !node.approved).length,
    pendingUsers: (users.data?.users ?? []).filter((user) => !user.approved).length,
  };
}

function PendingBadge({
  item,
  counts,
}: {
  readonly item: NavItem;
  readonly counts: PendingCounts;
}): ReactElement | null {
  if (item.badge === undefined) {
    return null;
  }

  const count = counts[item.badge];

  return count === 0 ? null : (
    <Sidebar.MenuBadge title={`${count} waiting for approval`}>{count}</Sidebar.MenuBadge>
  );
}

function Brand(): ReactElement {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2 px-2 group-data-[state=collapsed]/sidebar:justify-center group-data-[state=collapsed]/sidebar:px-0">
      <WaveformIcon className="size-5 shrink-0 text-kumo-brand" weight="duotone" />
      <span className="flex-1 truncate font-semibold text-kumo-strong group-data-[state=collapsed]/sidebar:hidden">
        headscale
      </span>
    </div>
  );
}

/** Breadcrumb trail: the section, then the page a detail route announced through useBreadcrumb. */
function Trail({ current }: { readonly current: NavItem | undefined }): ReactElement {
  const leaf = useBreadcrumbLeaf();

  return (
    <div className="flex min-w-0 items-center gap-2">
      <Sidebar.Trigger className="md:hidden" aria-label="Open navigation" />
      <Breadcrumbs>
        {current === undefined || leaf === null ? (
          <Breadcrumbs.Current>{current?.label ?? leaf ?? "headscale"}</Breadcrumbs.Current>
        ) : (
          <>
            <Breadcrumbs.Link href={current.to}>{current.label}</Breadcrumbs.Link>
            <Breadcrumbs.Separator />
            <Breadcrumbs.Current>{leaf}</Breadcrumbs.Current>
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
  session: "Signed in",
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
