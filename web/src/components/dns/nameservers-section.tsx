import { Button } from "@cloudflare/kumo/components/button";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { DnsSettings } from "~/api/schema.gen.ts";
import { EntryList } from "~/components/dns/entry-list.tsx";
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
import { Section, SectionRow } from "~/components/ui/section.tsx";

/** The id of the override paragraph, which explains why a disabled toggle is disabled. */
const overrideHelpId = "dns-override-local-help";

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
                size="sm"
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
        empty="No global nameservers. Machines keep using their own resolvers."
        entries={settings.nameservers.map((ns) => ({
          key: ns,
          value: ns,
          control: (
            <ExitNodeToggle
              name={`Use with exit node: ${ns}`}
              checked={keptWithExitNode(settings, ns)}
              disabled={!canEdit || pending || !settings.overrideLocalDns}
              describedBy={settings.overrideLocalDns ? undefined : overrideHelpId}
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
            to use with an exit node: a marked one stays in use while a machine routes through an
            exit node, and the rest of its DNS goes through the exit node then. Turning it off
            clears the marks.
          </p>
        </div>
        <span className="flex h-lh shrink-0 items-center">
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

/** The per-nameserver switch for keeping it while an exit node is selected. */
/**
 * "Use with exit node" as a labelled switch: the visible label is clickable, and the accessible
 * name adds the resolver or domain it belongs to.
 */
export function ExitNodeToggle({
  name,
  checked,
  disabled,
  describedBy,
  pending,
  onChange,
}: {
  readonly name: string;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly describedBy?: string | undefined;
  readonly pending: boolean;
  readonly onChange: (on: boolean) => void;
}): ReactElement {
  return (
    <Switch
      size="sm"
      label={<span className="text-sm text-kumo-subtle">Use with exit node</span>}
      controlFirst={false}
      aria-label={name}
      {...(describedBy === undefined ? {} : { "aria-describedby": describedBy })}
      checked={checked}
      disabled={disabled}
      transitioning={pending}
      onCheckedChange={onChange}
    />
  );
}
