import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { Sidebar } from "@cloudflare/kumo/components/sidebar";
import { Text } from "@cloudflare/kumo/components/text";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import {
  DesktopIcon,
  GearSixIcon,
  KeyIcon,
  ShieldCheckIcon,
  SignOutIcon,
  SquaresFourIcon,
  UsersIcon,
  WaveformIcon,
} from "@phosphor-icons/react";
import { useRouterState } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import { api } from "~/api/client.ts";
import type { Me, Scope } from "~/auth/me.ts";
import { can, displayName, roleLabel } from "~/auth/me.ts";
import { session } from "~/auth/session.ts";
import { ThemeToggle } from "~/components/layout/theme-toggle.tsx";

interface NavItem {
  readonly to: "/" | "/machines" | "/users" | "/keys" | "/policy" | "/settings";
  readonly label: string;
  readonly icon: Icon;
  /** Hidden without this scope; members without any scope still get their machines. */
  readonly scope?: Scope;
  readonly exact?: boolean;
}

const nav: readonly NavItem[] = [
  { to: "/", label: "Overview", icon: SquaresFourIcon, exact: true },
  { to: "/machines", label: "Machines", icon: DesktopIcon, scope: "devices:core:read" },
  { to: "/users", label: "Users", icon: UsersIcon, scope: "users:read" },
  { to: "/keys", label: "Keys", icon: KeyIcon },
  { to: "/policy", label: "Access controls", icon: ShieldCheckIcon, scope: "policy_file:read" },
  { to: "/settings", label: "Settings", icon: GearSixIcon, scope: "feature_settings:read" },
];

function isActive(item: NavItem, pathname: string): boolean {
  return item.exact === true ? pathname === item.to : pathname.startsWith(item.to);
}

export function Shell({
  me,
  children,
}: {
  readonly me: Me;
  readonly children: ReactNode;
}): ReactElement {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const items = nav.filter((item) => item.scope === undefined || can(me, item.scope));
  const current = items.find((item) => isActive(item, pathname));

  return (
    <Sidebar.Provider defaultOpen collapsible="offcanvas">
      <Sidebar className="sticky top-0 h-svh">
        <Sidebar.Header>
          <Brand />
        </Sidebar.Header>
        <Sidebar.Content>
          <Sidebar.Group>
            <Sidebar.Menu>
              {items.map((item) => (
                <Sidebar.MenuButton
                  key={item.to}
                  href={item.to}
                  icon={item.icon}
                  active={isActive(item, pathname)}
                  tooltip={item.label}
                >
                  {item.label}
                </Sidebar.MenuButton>
              ))}
            </Sidebar.Menu>
          </Sidebar.Group>
        </Sidebar.Content>
        <Sidebar.Footer>
          <HealthIndicator />
        </Sidebar.Footer>
      </Sidebar>
      <div className="flex min-h-svh min-w-0 flex-1 flex-col bg-kumo-canvas">
        <header className="sticky top-0 z-10 flex h-[58px] shrink-0 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-base px-4 lg:px-8">
          <div className="flex items-center gap-2">
            <Sidebar.Trigger />
            <Text bold>{current?.label ?? ""}</Text>
          </div>
          <div className="flex items-center gap-1">
            <ThemeToggle />
            <AccountMenu me={me} />
          </div>
        </header>
        <main className="flex-1 px-4 py-6 lg:px-8">
          <div className="mx-auto flex w-full max-w-6xl flex-col gap-6">{children}</div>
        </main>
      </div>
    </Sidebar.Provider>
  );
}

function Brand(): ReactElement {
  return (
    <div className="flex items-center gap-2 px-1">
      <span className="flex size-7 items-center justify-center rounded-md bg-kumo-contrast text-kumo-inverse">
        <WaveformIcon className="size-4" weight="bold" />
      </span>
      <Text bold>headscale</Text>
    </div>
  );
}

function HealthIndicator(): ReactElement {
  const health = api.useQuery("get", "/api/v1/health", undefined, { refetchInterval: 30_000 });
  const state = healthState(health.isPending, health.data?.databaseConnectivity === true);

  return (
    <div className="flex items-center gap-2 px-2 py-1 text-xs text-kumo-subtle">
      <span aria-hidden className={cn("size-2 rounded-full", state.dot)} />
      {state.label}
    </div>
  );
}

function healthState(pending: boolean, ok: boolean): { label: string; dot: string } {
  if (pending) {
    return { label: "Checking server…", dot: "bg-kumo-inactive" };
  }

  return ok
    ? { label: "Server healthy", dot: "bg-kumo-success" }
    : { label: "Server unhealthy", dot: "bg-kumo-danger" };
}

const kindLabels: Record<string, string> = {
  oauth: "OAuth token",
  local: "Local socket",
  api_key: "API key",
};

function AccountMenu({ me }: { readonly me: Me }): ReactElement {
  const name = displayName(me);
  const role = roleLabel(me);
  const initial = name.slice(0, 1).toUpperCase();

  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="ghost" shape="circle" size="sm" aria-label="Account" title={name}>
            <span className="text-xs font-medium">{initial}</span>
          </Button>
        }
      />
      <DropdownMenu.Content align="end" className="min-w-56">
        <DropdownMenu.Group>
          <DropdownMenu.Label className="flex flex-col gap-0.5">
            <span className="font-medium text-kumo-default">{name}</span>
            <span className="text-kumo-subtle">
              {kindLabels[me.kind] ?? me.kind}
              {role === null ? "" : ` · ${role}`}
            </span>
          </DropdownMenu.Label>
          <DropdownMenu.Separator />
          <DropdownMenu.Item
            icon={SignOutIcon}
            onClick={() => {
              session.clear();
            }}
          >
            Sign out
          </DropdownMenu.Item>
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
