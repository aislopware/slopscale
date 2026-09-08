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
          aside: (
            <ExitNodeToggle
              label={`Use ${ns} with an exit node`}
              checked={keptWithExitNode(settings, ns)}
              disabled={!canEdit || pending || !settings.overrideLocalDns}
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
          <p className="max-w-prose text-kumo-subtle">
            Machines use the nameservers above for every query instead of only when their own
            resolvers cannot answer. Needs at least one nameserver. A nameserver marked to use with
            an exit node stays in use while a machine routes through one; the rest of its DNS goes
            through the exit node then.
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
        mutations={mutations}
        onSubmit={(value, done) => {
          mutations.set.mutate({ body: withNameserver(settings, value) }, { onSuccess: done });
        }}
      />
    </Section>
  );
}

/** The per-nameserver switch for keeping it while an exit node is selected. */
export function ExitNodeToggle({
  label,
  checked,
  disabled,
  pending,
  onChange,
}: {
  readonly label: string;
  readonly checked: boolean;
  readonly disabled: boolean;
  readonly pending: boolean;
  readonly onChange: (on: boolean) => void;
}): ReactElement {
  return (
    <span className="flex items-center gap-2 text-xs text-kumo-subtle">
      Use with exit node
      <Switch
        size="sm"
        aria-label={label}
        checked={checked}
        disabled={disabled}
        transitioning={pending}
        onCheckedChange={onChange}
      />
    </span>
  );
}
