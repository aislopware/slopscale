import type { Icon } from "@phosphor-icons/react";
import {
  AppWindowIcon,
  ClockCounterClockwiseIcon,
  DesktopIcon,
  GearSixIcon,
  GlobeIcon,
  HandWavingIcon,
  HardDrivesIcon,
  BroadcastIcon,
  KeyIcon,
  WebhooksLogoIcon,
  PathIcon,
  ShieldCheckIcon,
  SignpostIcon,
  SquaresFourIcon,
  TerminalWindowIcon,
  UsersIcon,
} from "@phosphor-icons/react";

import type { Me, Scope } from "~/auth/me.ts";
import { can } from "~/auth/me.ts";

export type NavPath =
  | "/"
  | "/machines"
  | "/users"
  | "/keys"
  | "/keys/pre-auth"
  | "/keys/api"
  | "/keys/oauth"
  | "/access"
  | "/policy"
  | "/policy/rules"
  | "/policy/graph"
  | "/policy/groups"
  | "/policy/postures"
  | "/policy/requests"
  | "/policy/file"
  | "/dns"
  | "/dns/nameservers"
  | "/dns/split"
  | "/dns/records"
  | "/relays"
  | "/relays/map"
  | "/relays/latency"
  | "/relays/sources"
  | "/relays/own"
  | "/relays/embedded"
  | "/networks"
  | "/routes"
  | "/services"
  | "/apps"
  | "/integrations"
  | "/integrations/webhooks"
  | "/integrations/log-streams"
  | "/integrations/posture"
  | "/settings"
  | "/settings/tailnet"
  | "/settings/sessions"
  | "/settings/server"
  | "/audit"
  | "/sessions";

/** The live counts the sidebar can show next to an item. */
export type NavBadge = "pendingNodes" | "pendingUsers" | "pendingRoutes" | "pendingRequests";

/** A page under a branch of the sidebar. It has no icon of its own; the indent says where it is. */
export interface NavChild {
  readonly to: NavPath;
  readonly label: string;
  /** Hidden without this scope. */
  readonly scope?: Scope;
  /** Which live count the sidebar shows next to the page. */
  readonly badge?: NavBadge;
}

export interface NavItem {
  readonly to: NavPath;
  readonly label: string;
  readonly icon: Icon;
  /** Hidden without this scope; members without any scope still get their machines. */
  readonly scope?: Scope;
  readonly exact?: boolean;
  /** Which live count the sidebar shows next to the item. */
  readonly badge?: NavBadge;
  /**
   * The pages under this item. An item with children is a branch that opens and closes rather than
   * a page: its own path sends the browser to the first child the caller may see.
   */
  readonly children?: readonly NavChild[];
}

export interface NavGroup {
  readonly label?: string;
  readonly items: readonly NavItem[];
}

/**
 * The sidebar, grouped by what the operator is doing: the machines and people on the tailnet, who
 * may reach what, how packets and names travel, what happened, and the switches, credentials and
 * outbound integrations that configure all of it. A page with several parts of its own is a branch
 * with a page per part, so every part has an address and a place in the sidebar.
 */
export const navGroups: readonly NavGroup[] = [
  { items: [{ to: "/", label: "Overview", icon: SquaresFourIcon, exact: true }] },
  {
    label: "Tailnet",
    items: [
      {
        to: "/machines",
        label: "Machines",
        icon: DesktopIcon,
        scope: "devices:core:read",
        badge: "pendingNodes",
      },
      { to: "/users", label: "Users", icon: UsersIcon, scope: "users:read", badge: "pendingUsers" },
    ],
  },
  {
    label: "Access",
    items: [
      {
        to: "/policy",
        label: "Access controls",
        icon: ShieldCheckIcon,
        scope: "policy_file:read",
        children: [
          { to: "/policy/rules", label: "Rules" },
          { to: "/policy/graph", label: "Graph" },
          { to: "/policy/groups", label: "Groups" },
          { to: "/policy/postures", label: "Postures" },
          { to: "/policy/requests", label: "Requests", badge: "pendingRequests" },
          { to: "/policy/file", label: "Policy file" },
        ],
      },
      { to: "/access", label: "My access", icon: HandWavingIcon },
    ],
  },
  {
    label: "Connectivity",
    items: [
      { to: "/networks", label: "Networks", icon: PathIcon, scope: "devices:routes:read" },
      {
        to: "/routes",
        label: "Routes",
        icon: SignpostIcon,
        scope: "devices:routes:read",
        badge: "pendingRoutes",
      },
      { to: "/services", label: "Services", icon: HardDrivesIcon, scope: "services:read" },
      { to: "/apps", label: "Apps", icon: AppWindowIcon, scope: "policy_file:read" },
      {
        to: "/dns",
        label: "DNS",
        icon: GlobeIcon,
        scope: "dns:read",
        children: [
          { to: "/dns/nameservers", label: "Nameservers" },
          { to: "/dns/split", label: "Split DNS" },
          { to: "/dns/records", label: "Extra records" },
        ],
      },
      {
        to: "/relays",
        label: "Relays",
        icon: BroadcastIcon,
        scope: "feature_settings:read",
        children: [
          { to: "/relays/map", label: "Map" },
          { to: "/relays/latency", label: "Latency" },
          { to: "/relays/sources", label: "Sources" },
          { to: "/relays/own", label: "Your relays" },
          { to: "/relays/embedded", label: "Embedded relay" },
        ],
      },
    ],
  },
  {
    label: "Logs",
    items: [
      {
        to: "/audit",
        label: "Audit log",
        icon: ClockCounterClockwiseIcon,
        scope: "logs:configuration:read",
      },
      {
        to: "/sessions",
        label: "SSH sessions",
        icon: TerminalWindowIcon,
        scope: "logs:configuration:read",
      },
    ],
  },
  {
    label: "Administration",
    items: [
      {
        to: "/settings",
        label: "Settings",
        icon: GearSixIcon,
        scope: "feature_settings:read",
        children: [
          { to: "/settings/tailnet", label: "Tailnet" },
          { to: "/settings/sessions", label: "Sessions" },
          { to: "/settings/server", label: "Server" },
        ],
      },
      {
        to: "/keys",
        label: "Keys",
        icon: KeyIcon,
        children: [
          { to: "/keys/pre-auth", label: "Pre-auth keys", scope: "auth_keys:read" },
          { to: "/keys/api", label: "API keys" },
          { to: "/keys/oauth", label: "OAuth clients", scope: "oauth_keys:read" },
        ],
      },
      {
        to: "/integrations",
        label: "Integrations",
        icon: WebhooksLogoIcon,
        children: [
          { to: "/integrations/webhooks", label: "Webhooks", scope: "webhooks:read" },
          {
            to: "/integrations/log-streams",
            label: "Log streams",
            scope: "logs:configuration:read",
          },
          {
            to: "/integrations/posture",
            label: "Device posture",
            scope: "devices:posture_attributes:read",
          },
        ],
      },
    ],
  },
];

/** The groups and pages the caller may see. A branch keeps only the children it may see. */
export function visibleGroups(me: Me): NavGroup[] {
  const groups: NavGroup[] = [];

  for (const group of navGroups) {
    const items = group.items
      .filter((item) => item.scope === undefined || can(me, item.scope))
      .map((item) => visibleItem(item, me))
      .filter((item) => item !== null);

    if (items.length > 0) {
      groups.push(group.label === undefined ? { items } : { label: group.label, items });
    }
  }

  return groups;
}

function visibleItem(item: NavItem, me: Me): NavItem | null {
  if (item.children === undefined) {
    return item;
  }

  const children = item.children.filter(
    (child) => child.scope === undefined || can(me, child.scope),
  );

  return children.length === 0 ? null : { ...item, children };
}

export function isActive(item: NavItem | NavChild, pathname: string): boolean {
  const exact = "exact" in item ? (item.exact ?? false) : false;

  return exact ? pathname === item.to : pathname === item.to || pathname.startsWith(`${item.to}/`);
}

/** Where the path is in the sidebar: the item, and the child under it when it is a branch. */
export interface NavPlace {
  readonly item: NavItem;
  readonly child?: NavChild;
}

export function placeOf(groups: readonly NavGroup[], pathname: string): NavPlace | undefined {
  const item = groups
    .flatMap((group) => group.items)
    .find((candidate) => isActive(candidate, pathname));

  if (item === undefined) {
    return undefined;
  }

  const child = item.children?.find((candidate) => isActive(candidate, pathname));

  return child === undefined ? { item } : { item, child };
}

/** A page as quick search lists it: a branch's children stand in for the branch. */
export interface NavPage {
  readonly to: NavPath;
  readonly label: string;
  readonly icon: Icon;
  /** The branch the page is under, so "Rules" says which rules. */
  readonly hint?: string;
}

export function pagesOf(groups: readonly NavGroup[]): NavPage[] {
  return groups.flatMap((group) =>
    group.items.flatMap((item): NavPage[] =>
      item.children === undefined
        ? [{ to: item.to, label: item.label, icon: item.icon }]
        : item.children.map((child) => ({
            to: child.to,
            label: child.label,
            icon: item.icon,
            hint: item.label,
          })),
    ),
  );
}
