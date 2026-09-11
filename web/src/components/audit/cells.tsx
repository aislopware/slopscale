import { Button } from "@cloudflare/kumo/components/button";
import { Tooltip } from "@cloudflare/kumo/components/tooltip";
import { cn } from "@cloudflare/kumo/utils";
import type { Icon } from "@phosphor-icons/react";
import {
  DesktopIcon,
  GearSixIcon,
  KeyIcon,
  TerminalWindowIcon,
  UserIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { Fragment } from "react";
import type { ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { Badge } from "~/components/ui/badge.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";
import { ValueList } from "~/components/ui/value-list.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

/** How the API names each kind of actor, in the console's words. */
const actorKinds: Record<string, string> = {
  api_key: "API key",
  local: "CLI",
  node: "Machine",
  oauth: "OAuth token",
  session: "Console session",
  system: "System",
};

/**
 * The mark for an actor that is not a person: the kind of credential, or the server. A person, a
 * key or a session bound to a user, keeps their initials.
 */
const actorIcons: Record<string, Icon> = {
  api_key: KeyIcon,
  local: TerminalWindowIcon,
  node: DesktopIcon,
  oauth: KeyIcon,
  session: UserIcon,
  system: GearSixIcon,
};

/**
 * Verbs after which the thing is gone or the request was turned down: the rows an operator scans a
 * log for, so they alone take the danger colour.
 */
const destructiveVerbs: ReadonlySet<string> = new Set([
  "delete",
  "deny",
  "expire",
  "reject",
  "revoke",
  "unshare",
]);

/** How the API names each kind of target, in the console's words. */
const targetKinds: Record<string, string> = {
  access_request: "Access request",
  access_rule: "Access rule",
  apikey: "API key",
  app: "App",
  derp: "Relays",
  dns: "DNS",
  dns_rule: "DNS rule",
  group: "Group",
  invite: "Invitation",
  key: "Key",
  logstream: "Log stream",
  network: "Network",
  node: "Machine",
  oauth_client: "OAuth client",
  oauthclient: "OAuth client",
  policy: "Policy",
  posture: "Posture",
  posture_integration: "Posture integration",
  preauthkey: "Pre-auth key",
  service: "Service",
  session: "Session",
  settings: "Settings",
  sshrecording: "SSH recording",
  user: "User",
  webhook: "Webhook",
};

export const clientError = 400;

/** At most this many detail fields per row; the rest are counted. */
const maxDetailFields = 2;
/** A whole RFC 3339 timestamp with its zone, as the API writes them; anything looser stays text. */
const timestampPattern =
  /^(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/u;

/**
 * A name that leads somewhere reads as a link without turning the column blue: it only takes the
 * link colour and the underline on hover or keyboard focus.
 */
const linkClass =
  "block truncate font-medium text-kumo-default hover:text-kumo-link hover:underline focus-visible:underline";

/** The name to show for an actor: a person if there is one, else what kind of credential acted. */
export function actorName(event: AuditEvent): string {
  if (event.actorName !== "") {
    return event.actorName;
  }

  return actorKinds[event.actorKind] ?? "Unknown";
}

/** The kind, as a second line; null when it would only repeat the name. */
function actorKindLine(event: AuditEvent): string | null {
  const label = actorKinds[event.actorKind] ?? event.actorKind;

  return label === "" || label === actorName(event) ? null : label;
}

export function ActorCell({ event }: { readonly event: AuditEvent }): ReactElement {
  const name = actorName(event);
  const kind = actorKindLine(event);
  const icon = event.actorUserId === "" ? (actorIcons[event.actorKind] ?? UserIcon) : undefined;

  return (
    <div className="flex min-w-0 items-start gap-2">
      <span className="flex h-lh items-center">
        <Avatar name={name} size="sm" {...(icon === undefined ? {} : { icon })} />
      </span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-kumo-default">{name}</span>
        {kind === null ? null : <span className="truncate text-xs text-kumo-subtle">{kind}</span>}
      </div>
    </div>
  );
}

/**
 * The action as the API spells it, object first and verb last: the object steps back and the verb
 * carries the line, so "node.delete" and "node.rename" are told apart by the word that differs. A
 * break opportunity after each dot lets a long name wrap at a segment instead of running into the
 * next column; anywhere is the last resort for one segment wider than the column.
 */
export function ActionCell({ event }: { readonly event: AuditEvent }): ReactElement {
  const segments = event.action.split(".");
  const verb = segments.pop() ?? "";

  return (
    <code className="font-mono text-[0.9em] [overflow-wrap:anywhere]">
      {segments.map((segment, index) => (
        <Fragment key={segments.slice(0, index + 1).join(".")}>
          <span className="text-kumo-subtle">{`${segment}.`}</span>
          <wbr />
        </Fragment>
      ))}
      <span className={destructiveVerbs.has(verb) ? "text-kumo-danger" : "text-kumo-default"}>
        {verb}
      </span>
    </code>
  );
}

function TargetLink({
  event,
  name,
}: {
  readonly event: AuditEvent;
  readonly name: string;
}): ReactElement {
  if (event.targetKind === "node" && event.targetId !== "") {
    return (
      <Link to="/machines/$nodeId" params={{ nodeId: event.targetId }} className={linkClass}>
        {name}
      </Link>
    );
  }

  if (event.targetKind === "user" && event.targetName !== "") {
    return (
      <Link to="/users" search={{ q: event.targetName }} className={linkClass}>
        {name}
      </Link>
    );
  }

  return <span className="block truncate text-kumo-default">{name}</span>;
}

export function TargetCell({ event }: { readonly event: AuditEvent }): ReactNode {
  if (event.targetKind === "" && event.targetId === "" && event.targetName === "") {
    return null;
  }

  const name = event.targetName === "" ? event.targetId : event.targetName;
  const kind = targetKinds[event.targetKind] ?? event.targetKind;

  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <TargetLink event={event} name={name} />
      {kind === "" ? null : <span className="truncate text-xs text-kumo-subtle">{kind}</span>}
    </div>
  );
}

/**
 * Whether the call did what it was asked. The exact status code is a detail, so it lives in the
 * expanded row and the cell says only what an operator scans for.
 */
export function ResultCell({ event }: { readonly event: AuditEvent }): ReactElement {
  const failed = event.outcome >= clientError;

  return <Badge tone={failed ? "danger" : "success"}>{failed ? "Failed" : "Success"}</Badge>;
}

/**
 * A detail field in the operator's words: timestamps as local dates, booleans as yes/no, lists
 * joined, and anything else as JSON. The raw value stays available for copying.
 */
function detailText(value: unknown): string {
  if (typeof value === "string") {
    const date = timestamp(value);

    return date === null ? value : formatAbsolute(date);
  }

  if (typeof value === "boolean") {
    return value ? "yes" : "no";
  }

  if (typeof value === "number") {
    return String(value);
  }

  if (value === null || value === undefined) {
    return "none";
  }

  if (Array.isArray(value)) {
    return value.length === 0 ? "none" : value.map((item) => detailText(item)).join(", ");
  }

  return JSON.stringify(value);
}

/**
 * The string as a date, or null when it is not a timestamp or names a day that does not exist: Date
 * accepts "2026-02-30" and rolls it into March, which would silently rewrite a value.
 */
function timestamp(value: string): Date | null {
  const match = timestampPattern.exec(value);
  const date = match === null ? null : parseTime(value);

  if (match?.groups === undefined || date === null) {
    return null;
  }

  const month = Number(match.groups["month"]);
  const day = Number(match.groups["day"]);
  const calendar = new Date(Date.UTC(Number(match.groups["year"]), month - 1, day));

  return calendar.getUTCMonth() === month - 1 && calendar.getUTCDate() === day ? date : null;
}

/** The value as the server sent it, for the copy button. */
function rawText(value: unknown): string {
  return typeof value === "string" ? value : (JSON.stringify(value) ?? String(value));
}

/**
 * The first couple of detail fields as key=value lines. The rest are counted in a "+N" button that
 * names them on hover; the row itself opens on click, so every field stays one click away and the
 * column keeps its width.
 */
export function DetailCell({
  event,
  onOpen,
}: {
  readonly event: AuditEvent;
  /** Opens the row: the "+N" button is its own, so the row's click does not reach it. */
  readonly onOpen: () => void;
}): ReactNode {
  const fields = Object.entries(event.detail);

  if (fields.length === 0) {
    return null;
  }

  const shown = fields.slice(0, maxDetailFields);
  const hidden = fields.slice(maxDetailFields).map(([key]) => key);

  return (
    <div className="flex min-w-0 items-start gap-2">
      <ValueList items={shown.map(([key, value]) => `${key}=${detailText(value)}`)} mono truncate />
      {hidden.length === 0 ? null : (
        <Tooltip
          content={hidden.join(", ")}
          render={
            <Button variant="secondary" size="xs" onClick={onOpen}>
              {`+${hidden.length}`}
            </Button>
          }
        />
      )}
    </div>
  );
}

function detailItems(event: AuditEvent): readonly Definition[] {
  const fields: Definition[] = Object.entries(event.detail).map(([key, value]) => ({
    key,
    label: key,
    value: detailText(value),
    copy: rawText(value),
  }));

  fields.push({ key: "outcome", label: "HTTP status", value: String(event.outcome) });

  if (event.remoteAddr !== "") {
    fields.push({
      key: "remoteAddr",
      label: "Remote address",
      value: event.remoteAddr,
      copy: event.remoteAddr,
    });
  }

  return fields;
}

/** The expanded row: every detail field the server recorded, plus where the call came from. */
export function EventDetail({ event }: { readonly event: AuditEvent }): ReactElement {
  const items = detailItems(event);
  // Narrow columns keep each label beside its value instead of across the whole table.
  const wide = items.length > 2;

  return (
    <div className={cn("py-1", wide ? "max-w-3xl" : "max-w-sm")}>
      <DefinitionList items={items} columns={wide ? 2 : 1} wrap />
    </div>
  );
}
