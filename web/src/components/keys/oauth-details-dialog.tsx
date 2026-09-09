import type { ReactElement } from "react";

import type { OAuthClient } from "~/api/queries.ts";
import { issuerHost } from "~/components/keys/federated.ts";
import { scopeLabel } from "~/components/keys/scopes.ts";
import { CopyText } from "~/components/ui/copy-text.tsx";
import { DialogContent, DialogRoot } from "~/components/ui/dialog.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

/**
 * Everything the trust conditions say, in full and click-to-copy. The list truncates a federated
 * subject to fit its column and editing is the only other place the whole value appears, which
 * leaves an operator who may read but not change OAuth clients with no way to see what a workload
 * has to present. This dialog reads; it changes nothing, so it opens for anyone who can see the
 * row.
 */
export function OAuthClientDetailsDialog({
  client,
  open,
  onOpenChange,
}: {
  readonly client: OAuthClient;
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
}): ReactElement {
  const federated = client.keyType === "federated";

  return (
    <DialogRoot open={open} onOpenChange={onOpenChange}>
      <DialogContent
        size="lg"
        title={federated ? "Federated identity" : "OAuth client"}
        description={
          federated
            ? "What a workload's OIDC token has to say before the server trades it for an API token."
            : "What this client's tokens may do."
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Client ID" value={client.clientId} />
          {client.description === "" ? null : (
            <Field label="Description" value={client.description} />
          )}
          {federated ? <TrustConditions client={client} /> : null}
          <Values label="Scopes" values={client.scopes.map(scopeLabel)} empty="None" />
          <Values label="Tags" values={client.tags} empty="None" mono />
          <Field label="Created" value={createdLabel(client.createdAt)} copyable={false} />
        </div>
      </DialogContent>
    </DialogRoot>
  );
}

function TrustConditions({ client }: { readonly client: OAuthClient }): ReactElement {
  const rules = Object.entries(client.customClaimRules);
  const host = issuerHost(client.issuer);

  return (
    <>
      <Field
        label="Issuer"
        value={client.issuer}
        {...(host === "" || host === client.issuer ? {} : { note: host })}
      />
      <Field label="Audience" value={client.audience} />
      <Field label="Subject" value={client.subject} />
      <div className="flex flex-col gap-1.5">
        <span className="text-sm font-medium text-kumo-default">Claim rules</span>
        {rules.length === 0 ? (
          <span className="text-sm text-kumo-subtle">
            No further claims; the subject and audience are the whole condition.
          </span>
        ) : (
          <div className="flex flex-col gap-1.5">
            {rules.map(([claim, value]) => (
              <Field key={claim} label={claim} value={value} mono />
            ))}
          </div>
        )}
      </div>
    </>
  );
}

/** One value with its own copy button, since each of these is pasted somewhere else in turn. */
function Field({
  label,
  value,
  note,
  mono = false,
  copyable = true,
}: {
  readonly label: string;
  readonly value: string;
  /** A shorter reading of the value, such as the host of a long issuer URL. */
  readonly note?: string;
  readonly mono?: boolean;
  readonly copyable?: boolean;
}): ReactElement {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className={labelClass(mono)}>{label}</span>
      {copyable ? (
        <CopyText
          value={value}
          label={`Copy ${label.toLowerCase()}`}
          wrap
          className="w-full items-start justify-between gap-2 text-left"
        />
      ) : (
        <span className="text-sm text-kumo-default">{value}</span>
      )}
      {note === undefined ? null : <span className="text-sm text-kumo-subtle">{note}</span>}
    </div>
  );
}

function labelClass(mono: boolean): string {
  return mono ? "font-mono text-[0.9em] text-kumo-subtle" : "text-sm font-medium text-kumo-default";
}

/** A list read as words: scopes by their console names, tags as the policy spells them. */
function Values({
  label,
  values,
  empty,
  mono = false,
}: {
  readonly label: string;
  readonly values: readonly string[];
  readonly empty: string;
  readonly mono?: boolean;
}): ReactElement {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-kumo-default">{label}</span>
      {values.length === 0 ? (
        <span className="text-sm text-kumo-subtle">{empty}</span>
      ) : (
        <span className={valueClass(mono)}>{values.join(", ")}</span>
      )}
    </div>
  );
}

function valueClass(mono: boolean): string {
  return mono ? "font-mono text-[0.9em] text-kumo-default" : "text-sm text-kumo-default";
}

function createdLabel(createdAt: string | null): string {
  const date = parseTime(createdAt);

  return date === null ? "Unknown" : formatAbsolute(date);
}
