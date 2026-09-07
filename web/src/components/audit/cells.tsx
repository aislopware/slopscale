import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { cn } from "@cloudflare/kumo/utils";
import { Link } from "@tanstack/react-router";
import type { ReactElement, ReactNode } from "react";

import type { AuditEvent } from "~/api/queries.ts";
import { Avatar } from "~/components/ui/avatar.tsx";
import { DefinitionList } from "~/components/ui/definition-list.tsx";
import type { Definition } from "~/components/ui/definition-list.tsx";

/** How the API names each kind of actor, in the console's words. */
const actorKinds: Record<string, string> = {
  api_key: "API key",
  local: "CLI",
  oauth: "OAuth token",
  session: "Console session",
  system: "System",
};

/** How the API names each kind of target, in the console's words. */
const targetKinds: Record<string, string> = {
  apikey: "API key",
  key: "Key",
  node: "Node",
  policy: "Policy",
  preauthkey: "Pre-auth key",
  session: "Session",
  settings: "Settings",
  user: "User",
};

export const clientError = 400;

/** At most this many detail fields per row; the rest are counted. */
const maxDetailFields = 2;
/** Longer detail values are cut, so one long field cannot push the row open. */
const maxDetailLength = 24;

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

  return (
    <div className="flex min-w-0 items-start gap-2">
      <span className="flex h-lh items-center">
        <Avatar name={name} size="sm" />
      </span>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="truncate text-kumo-default">{name}</span>
        {kind === null ? null : <span className="truncate text-xs text-kumo-subtle">{kind}</span>}
      </div>
    </div>
  );
}

export function ActionCell({ event }: { readonly event: AuditEvent }): ReactElement {
  return <code className="font-mono text-[0.9em] text-kumo-default">{event.action}</code>;
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

  return (
    <Badge appearance="dot" variant={failed ? "error" : "success"}>
      {failed ? "Failed" : "Success"}
    </Badge>
  );
}

/** A detail field as one short string; anything that is not a primitive is shown as JSON. */
function detailText(value: unknown): string {
  return primitive(value) ?? JSON.stringify(value) ?? String(value);
}

function primitive(value: unknown): string | undefined {
  if (typeof value === "string") {
    return value;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  return undefined;
}

function clip(text: string): string {
  return text.length > maxDetailLength ? `${text.slice(0, maxDetailLength)}…` : text;
}

/**
 * The first couple of detail fields as chips. The rest are behind "+N more", which opens the row
 * rather than widening it, so every field stays one click away and the column keeps its width.
 */
export function DetailCell({
  event,
  onOpen,
}: {
  readonly event: AuditEvent;
  readonly onOpen: () => void;
}): ReactNode {
  const fields = Object.entries(event.detail);

  if (fields.length === 0) {
    return null;
  }

  const shown = fields.slice(0, maxDetailFields);
  const hidden = fields.length - shown.length;

  return (
    <div className="flex flex-wrap items-center gap-1">
      {shown.map(([key, value]) => (
        <Badge key={key} variant="secondary" className="font-mono font-normal">
          {key}={clip(detailText(value))}
        </Badge>
      ))}
      {hidden === 0 ? null : (
        <Button variant="ghost" size="xs" onClick={onOpen}>
          +{hidden} more
        </Button>
      )}
    </div>
  );
}

function detailItems(event: AuditEvent): readonly Definition[] {
  const fields: Definition[] = Object.entries(event.detail).map(([key, value]) => {
    const text = detailText(value);

    return { key, label: key, value: text, copy: text };
  });

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
