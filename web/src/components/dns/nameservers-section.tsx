import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { DnsSettings } from "~/api/schema.gen.ts";
import { EntryActionSpacer, EntryList } from "~/components/dns/entry-list.tsx";
import {
  keptWithExitNode,
  nameserverError,
  withNameserver,
  withOverrideLocalDns,
  withUseWithExitNode,
  withoutNameserver,
} from "~/components/dns/model.ts";
import type { DnsMutations } from "~/components/dns/mutations.ts";
import { ValueDialog } from "~/components/dns/value-dialog.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";

/** The id of the override paragraph, which explains why a disabled toggle is disabled. */
const overrideHelpId = "dns-override-local-help";

/** Why "Use with exit node" is off limits, or nothing when it is not. */
function toggleReason(canEdit: boolean, overrideLocalDns: boolean): string | undefined {
  if (!canEdit) {
    return "Your credentials may not change DNS";
  }

  return overrideLocalDns ? undefined : "Turn on Override local DNS to mark a nameserver";
}

export function NameserversSection({
  settings,
  canEdit,
  mutations,
}: {
  readonly settings: DnsSettings;
  readonly canEdit: boolean;
  readonly mutations: DnsMutations;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Nameservers"
      description="Global resolvers every machine uses for names outside the tailnet."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <Button
                variant="secondary"
                icon={PlusIcon}
                onClick={() => {
                  setAdding(true);
                }}
              >
                Add nameserver
              </Button>
            ),
          }
        : {})}
    >
      <EntryList
        canEdit={canEdit}
        pending={pending}
        empty={{
          title: "No global nameservers",
          description: "Machines keep using the resolvers they already have.",
        }}
        entries={settings.nameservers.map((ns) => ({
          key: ns,
          value: ns,
          control: (
            <ExitNodeToggle
              name={ns}
              checked={keptWithExitNode(settings, ns)}
              disabled={!canEdit || pending || !settings.overrideLocalDns}
              describedBy={settings.overrideLocalDns ? undefined : overrideHelpId}
              reason={toggleReason(canEdit, settings.overrideLocalDns)}
              pending={pending}
              onChange={(on) => {
                mutations.apply(
                  withUseWithExitNode(settings, ns, on),
                  `${ns} ${on ? "kept" : "dropped"} with an exit node`,
                );
              }}
            />
          ),
          removeLabel: `Remove nameserver ${ns}`,
          onRemove: () => {
            mutations.apply(withoutNameserver(settings, ns), `Removed ${ns}`);
          },
        }))}
      />
      <SectionRow className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="font-medium text-kumo-strong">Override local DNS</span>
          <p id={overrideHelpId} className="max-w-prose text-kumo-subtle">
            Machines use the nameservers above for every query instead of only when their own
            resolvers cannot answer. Needs at least one nameserver. Turn it on to mark nameservers
            to use with an exit node. A marked one stays in use while a machine routes through an
            exit node, and the rest of its DNS goes through the exit node then. Turning it off
            clears the marks.
          </p>
        </div>
        {/* The spacer stands where a nameserver row's remove button is, so both toggles end together. */}
        <span className="flex h-lh shrink-0 items-center gap-3">
          <Switch
            aria-label="Override local DNS"
            checked={settings.overrideLocalDns}
            disabled={
              !canEdit ||
              pending ||
              (settings.nameservers.length === 0 && !settings.overrideLocalDns)
            }
            transitioning={pending}
            onCheckedChange={(on) => {
              mutations.apply(
                withOverrideLocalDns(settings, on),
                `Override local DNS ${on ? "on" : "off"}`,
              );
            }}
          />
          {canEdit ? <EntryActionSpacer /> : null}
        </span>
      </SectionRow>
      <ValueDialog
        open={adding}
        onOpenChange={setAdding}
        title="Add nameserver"
        description="An IP address, an IP with port, or the DNS-over-HTTPS URL of a provider Tailscale knows, such as https://dns.nextdns.io/abc123."
        label="Nameserver"
        placeholder="1.1.1.1"
        validate={nameserverError}
        successMessage="Nameserver added"
        mutation={mutations.set}
        onSubmit={(value, done) => {
          mutations.set.mutate({ body: withNameserver(settings, value) }, { onSuccess: done });
        }}
      />
    </Section>
  );
}

/**
 * "Use with exit node" as a labelled switch: the visible label is clickable, and a hidden part of
 * it names the resolver or domain, because Kumo's Switch names itself by its label and would drop
 * an aria-label.
 */
export function ExitNodeToggle({
  name,
  checked,
  disabled,
  describedBy,
  reason,
  pending,
  onChange,
}: {
  readonly name: string;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly describedBy?: string | undefined;
  /** Why the toggle is off limits, shown on hover while it is disabled. */
  readonly reason?: string | undefined;
  readonly pending: boolean;
  readonly onChange: (on: boolean) => void;
}): ReactElement {
  return (
    <DisabledReason reason={disabled ? reason : undefined}>
      <Switch
        size="sm"
        label={
          <span className="text-sm text-kumo-subtle">
            Use with exit node<span className="sr-only"> for {name}</span>
          </span>
        }
        controlFirst={false}
        {...(describedBy === undefined ? {} : { "aria-describedby": describedBy })}
        checked={checked}
        disabled={disabled}
        transitioning={pending}
        onCheckedChange={onChange}
      />
    </DisabledReason>
  );
}
