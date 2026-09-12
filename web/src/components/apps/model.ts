import { isIpv4, isIpv6 } from "~/lib/ip.ts";

const maxIpv4Bits = 32;
const maxIpv6Bits = 128;
const maxDomainLength = 253;
const maxNameLength = 63;
const minTagLength = 5;
const labelPattern = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/iu;
const tagPattern = /^tag:[a-zA-Z0-9_-]+$/u;

export interface AppDraft {
  readonly name: string;
  readonly description: string;
  readonly domains: readonly string[];
  readonly connectors: readonly string[];
  readonly routes: readonly string[];
}

/** Formats a connector selector: * means all connectors. */
export function connectorLabel(connector: string): string {
  return connector === "*" ? "Every connector" : connector;
}

/**
 * Whether an app domain covers a domain a connector learned: itself, or for `*.example.com` any
 * name under it, which is the client's own rule (a wildcard does not cover its bare domain).
 */
export function domainCovers(pattern: string, domain: string): boolean {
  const wanted = normalizeDomain(pattern);
  const learned = normalizeDomain(domain);

  if (wanted.startsWith("*.")) {
    return learned.endsWith(wanted.slice(1));
  }

  return learned === wanted;
}

/**
 * How many routes the connectors have learned for an app: the distinct addresses across every
 * answer, counted once, for the domains the app covers. A connector serving several apps answers
 * with everything it learned, so the answer is narrowed to the app before counting.
 */
export function learnedForApp(
  domains: readonly string[],
  answers: readonly Readonly<Record<string, string[] | null>>[],
): number {
  const addresses = new Set<string>();

  for (const answer of answers) {
    for (const [domain, learned] of Object.entries(answer)) {
      if (domains.some((pattern) => domainCovers(pattern, domain))) {
        for (const address of learned ?? []) {
          addresses.add(address);
        }
      }
    }
  }

  return addresses.size;
}

/** Total unapproved routes waiting across connector nodes. */
export function totalPendingRoutes(nodes: readonly { readonly pending: number }[]): number {
  return nodes.reduce((sum, node) => sum + node.pending, 0);
}

/** Online and total machine counts for the connector nodes. */
export function connectorMachineCounts(nodes: readonly { readonly online: boolean }[]): {
  readonly online: number;
  readonly total: number;
} {
  const online = nodes.filter((node) => node.online).length;

  return { online, total: nodes.length };
}

/** Lowercases and strips trailing dots. */
export function normalizeDomain(value: string): string {
  return value.trim().toLowerCase().replace(/\.$/u, "");
}

/** Whether the text is a valid domain or wildcard domain (example.com or *.example.com). */
export function isAppDomain(value: string): boolean {
  const normalized = normalizeDomain(value);

  if (normalized === "" || normalized.length > maxDomainLength) {
    return false;
  }

  const bare = normalized.startsWith("*.") ? normalized.slice(2) : normalized;

  if (bare === "" || bare.includes("*")) {
    return false;
  }

  const parts = bare.split(".");

  if (parts.length < 2) {
    return false;
  }

  return parts.every((part) => labelPattern.test(part));
}

/** Why the domain is invalid, or null if valid. */
export function appDomainError(value: string): string | null {
  const trimmed = value.trim();

  if (trimmed === "") {
    return "Enter a domain.";
  }

  return isAppDomain(trimmed) ? null : "Enter a domain such as example.com or *.example.com.";
}

/** The first problem among the domains, or null. */
export function domainsError(domains: readonly string[]): string | null {
  for (const domain of domains) {
    const issue = appDomainError(domain);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

function maxBitsOf(address: string): number | null {
  if (isIpv4(address)) {
    return maxIpv4Bits;
  }

  return isIpv6(address) ? maxIpv6Bits : null;
}

/** Why the route is not a valid non-default CIDR, or null. */
export function appRouteError(value: string): string | null {
  const trimmed = value.trim();

  if (trimmed === "") {
    return "Enter a route CIDR.";
  }

  const [address, bits, ...rest] = trimmed.split("/");

  if (address === undefined || address === "" || bits === undefined || rest.length > 0) {
    return `"${trimmed}" is not a CIDR. Use e.g. 10.0.0.0/24.`;
  }

  const maxBits = maxBitsOf(address);

  if (maxBits === null) {
    return `"${trimmed}" is not a valid IP address or CIDR.`;
  }

  const length = Number(bits);
  const whole = /^\d{1,3}$/u.test(bits) && Number.isInteger(length);

  if (!whole || length > maxBits) {
    return `"${trimmed}" has an invalid prefix length (must be 1 to ${maxBits}).`;
  }

  if (length === 0) {
    return `"${trimmed}" is a default route. Default routes are not allowed.`;
  }

  return null;
}

/** The first problem among the routes, or null. */
export function routesError(routes: readonly string[]): string | null {
  for (const route of routes) {
    const issue = appRouteError(route);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

/** Prepends tag: if missing. */
export function normalizeConnectorTag(value: string): string {
  const trimmed = value.trim();

  if (trimmed === "" || trimmed === "*") {
    return "*";
  }

  return trimmed.startsWith("tag:") ? trimmed : `tag:${trimmed}`;
}

/** Validates a connector tag. */
export function connectorTagError(value: string): string | null {
  const trimmed = value.trim();

  if (trimmed === "" || trimmed === "*") {
    return null;
  }

  const tag = normalizeConnectorTag(trimmed);

  if (tag === "tag:" || tag.length < minTagLength) {
    return "Enter a valid tag name (e.g. tag:app).";
  }

  if (!tagPattern.test(tag)) {
    return `"${trimmed}" is not a valid tag. Use lower-case letters, numbers, and dashes.`;
  }

  return null;
}

/** The first problem among the connectors, or null. */
export function connectorsError(connectors: readonly string[]): string | null {
  for (const connector of connectors) {
    const issue = connectorTagError(connector);

    if (issue !== null) {
      return issue;
    }
  }

  return null;
}

/** Validates the app name. */
export function appNameError(name: string): string | null {
  const trimmed = name.trim();

  if (trimmed === "") {
    return "Enter a name.";
  }

  if (trimmed.length > maxNameLength) {
    return "Name must be 63 characters or fewer.";
  }

  return null;
}

/** Validates the whole app draft before submission. */
export function appValidationError(draft: AppDraft): string | null {
  const nameIssue = appNameError(draft.name);

  if (nameIssue !== null) {
    return nameIssue;
  }

  if (draft.domains.length === 0 && draft.routes.length === 0) {
    return "An app needs at least one domain or route.";
  }

  const domainIssue = domainsError(draft.domains);

  if (domainIssue !== null) {
    return domainIssue;
  }

  const routeIssue = routesError(draft.routes);

  if (routeIssue !== null) {
    return routeIssue;
  }

  const connectorIssue = connectorsError(draft.connectors);

  if (connectorIssue !== null) {
    return connectorIssue;
  }

  return null;
}

/** Summarizes total app count in sentence case. */
export function countApps(total: number): string {
  return total === 1 ? "1 app" : `${total} apps`;
}

/** Whether the selectors pick every connector: no tag at all, or the wildcard. */
export function everyConnector(connectors: readonly string[]): boolean {
  return connectors.length === 0 || connectors.includes("*");
}

/** "2/3" for the connector machines that are online out of the ones the selectors pick. */
export function machinesLabel(counts: { readonly online: number; readonly total: number }): string {
  return `${counts.online}/${counts.total}`;
}

/** The routes page's search for the routes an app is waiting on. */
export interface PendingRoutesSearch {
  /** The pending chip, always on: the operator followed the count to approve those routes. */
  readonly pending: true;
  /** The one connector waiting, when there is one. */
  readonly q?: string;
}

/**
 * What the routes page should be filtered by when the operator follows an app's pending count. The
 * pending chip is always on, because approving those routes is why the operator followed the link.
 * One connector waiting names that machine as well; several would need several filters, so the
 * pending routes of the whole tailnet are the closest the page can get.
 */
export function pendingSearch(
  nodes: readonly { readonly name: string; readonly pending: number }[],
): PendingRoutesSearch {
  const waiting = nodes.filter((node) => node.pending > 0);
  const only = waiting.length === 1 ? waiting[0] : undefined;

  return { pending: true, ...(only === undefined ? {} : { q: only.name }) };
}

/** The apps a machine serves as a connector, by name. */
export function appsForNode<
  TApp extends { readonly nodes: readonly { readonly nodeId: string }[] },
>(apps: readonly TApp[], nodeId: string): TApp[] {
  return apps.filter((app) => app.nodes.some((node) => node.nodeId === nodeId));
}

/** One domain the connector answers for, with every address it has resolved for it. */
export interface LearnedRoute {
  readonly domain: string;
  readonly addresses: readonly string[];
}

/**
 * What the connector has learned, a row per domain, alphabetically so the list holds still between
 * refreshes. A domain the client has resolved nothing for is kept: that it answers for it and found
 * nothing is the interesting case.
 */
export function learnedRoutes(domains: Readonly<Record<string, string[] | null>>): LearnedRoute[] {
  return Object.entries(domains)
    .map(([domain, addresses]) => ({ domain, addresses: addresses ?? [] }))
    .toSorted((left, right) => left.domain.localeCompare(right.domain));
}
