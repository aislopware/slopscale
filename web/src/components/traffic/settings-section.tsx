import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { useState } from "react";
import type { ReactElement, SubmitEvent } from "react";

import type { TrafficReporters, TrafficSettings } from "~/api/traffic.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { useTrafficSettingsMutation } from "~/components/traffic/mutations.ts";
import { TextLink } from "~/components/traffic/window-header.tsx";
import { refusalOf } from "~/components/traffic/window-refusal.tsx";
import { DisabledReason } from "~/components/ui/disabled-reason.tsx";
import { Section } from "~/components/ui/section.tsx";
import { Note } from "~/components/ui/status.tsx";
import { toast } from "~/components/ui/toast.ts";
import { ValueList } from "~/components/ui/value-list.tsx";

/** The bounds the server holds each retention to, so the form refuses what the server would. */
export const retentionBounds = {
  minuteHours: { min: 1, max: 168, unit: "hours" },
  hourDays: { min: 1, max: 90, unit: "days" },
  dayDays: { min: 1, max: 3650, unit: "days" },
} as const;

type RetentionKey = keyof typeof retentionBounds;

const retentionKeys: readonly RetentionKey[] = ["minuteHours", "hourDays", "dayDays"];

export type RetentionDraft = Record<RetentionKey, string>;

function draftOf(settings: TrafficSettings): RetentionDraft {
  return {
    minuteHours: String(settings.retention.minuteHours),
    hourDays: String(settings.retention.hourDays),
    dayDays: String(settings.retention.dayDays),
  };
}

const labels: Record<RetentionKey, string> = {
  minuteHours: "Per-minute totals",
  hourDays: "Hourly data",
  dayDays: "Daily data",
};

const descriptions: Record<RetentionKey, string> = {
  minuteHours: "The finest view of the rate chart, for windows of a few hours.",
  hourDays: "Hourly totals, destinations and names: what the week views read.",
  dayDays: "What the month and quarter views read.",
};

const hoursPerDay = 24;

/** Why the draft cannot be saved, or null when it can. The server enforces the same rules. */
export function retentionError(draft: RetentionDraft): string | null {
  for (const key of retentionKeys) {
    const { min, max, unit } = retentionBounds[key];
    const value = Number(draft[key]);

    if (draft[key] === "" || !Number.isInteger(value) || value < min || value > max) {
      return `Keep ${labels[key].toLowerCase()} for ${min} to ${max} ${unit}.`;
    }
  }

  if (Number(draft.minuteHours) > Number(draft.hourDays) * hoursPerDay) {
    return "Per-minute totals cannot outlive the hourly data.";
  }

  if (Number(draft.hourDays) > Number(draft.dayDays)) {
    return "Hourly data cannot outlive the daily data.";
  }

  return null;
}

/**
 * What the gateways collect and how long the server keeps it. The switches reach the agents with
 * their next report, within a minute.
 */
export function TrafficSettingsSections({
  settings,
  reporters,
  canEdit,
  canEditDns,
}: {
  readonly settings: TrafficSettings;
  readonly reporters: TrafficReporters;
  readonly canEdit: boolean;
  /** DNS logging rewrites every machine's resolvers, so it takes the DNS scope as well. */
  readonly canEditDns: boolean;
}): ReactElement {
  return (
    <>
      <Section
        title="Collection"
        description="What the agents on the gateways look at besides the connections themselves."
        bodyClassName="p-0"
      >
        <SniRow settings={settings} canEdit={canEdit} />
        <DnsRow settings={settings} reporters={reporters} canEdit={canEditDns} />
        <SettingRow
          title="Network names"
          description="Each destination's network and country, from iptoasn.com's table. The server loads it once a gateway has reported and downloads a newer one daily; the configuration file sets where from."
          control={
            <span className="text-kumo-subtle tabular-nums">
              {reporters.asnRanges === 0
                ? "Not loaded yet"
                : `${reporters.asnRanges.toLocaleString()} address ranges`}
            </span>
          }
        />
      </Section>
      <RetentionSection settings={settings} canEdit={canEdit} />
    </>
  );
}

function SniRow({
  settings,
  canEdit,
}: {
  readonly settings: TrafficSettings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useTrafficSettingsMutation();

  return (
    <SettingRow
      title="Name destinations from handshakes"
      description="Read the server name from each TLS and QUIC handshake that passes a gateway, so a destination shows as a host rather than an address. Nothing after the handshake is read."
      control={
        <DisabledReason reason={canEdit ? undefined : "Your credentials may not change this"}>
          <Switch
            aria-label="Name destinations from handshakes"
            checked={settings.sni}
            disabled={!canEdit || update.isPending}
            transitioning={update.isPending}
            onCheckedChange={(on) => {
              update.mutate(
                { body: { sni: on } },
                {
                  onSuccess: () => {
                    toast.success(on ? "Handshake names on" : "Handshake names off");
                  },
                },
              );
            }}
          />
        </DisabledReason>
      }
    />
  );
}

function DnsRow({
  settings,
  reporters,
  canEdit,
}: {
  readonly settings: TrafficSettings;
  readonly reporters: TrafficReporters;
  readonly canEdit: boolean;
}): ReactElement {
  // The server says why it will not switch DNS logging on, such as no nameserver to forward to;
  // that reason stays beside the switch rather than in a toast that goes away.
  const [refusal, setRefusal] = useState("");
  const update = useTrafficSettingsMutation({ quiet: true });

  return (
    <SettingRow
      title="Log DNS lookups"
      description={
        <DnsLoggingDescription
          reporters={reporters}
          on={settings.dnsLogging}
          refusal={refusal}
          canEdit={canEdit}
        />
      }
      control={
        <DisabledReason
          reason={canEdit ? undefined : "Changing this takes the traffic and DNS permissions"}
        >
          <Switch
            aria-label="Log DNS lookups"
            checked={settings.dnsLogging}
            disabled={!canEdit || update.isPending}
            transitioning={update.isPending}
            onCheckedChange={(on) => {
              setRefusal("");
              update.mutate(
                { body: { dnsLogging: on } },
                {
                  onSuccess: () => {
                    toast.success(on ? "DNS logging on" : "DNS logging off");
                  },
                  onError: (failure) => {
                    setRefusal(refusalOf(failure));
                  },
                },
              );
            }}
          />
        </DisabledReason>
      }
    />
  );
}

function DnsLoggingDescription({
  reporters,
  on,
  refusal,
  canEdit,
}: {
  readonly reporters: TrafficReporters;
  readonly on: boolean;
  readonly refusal: string;
  readonly canEdit: boolean;
}): ReactElement {
  return (
    <span className="flex flex-col gap-2">
      <span>
        Machines that accept the tailnet&apos;s DNS use one approved gateway resolver plus the
        global nameservers in place of their local DNS, so every lookup is logged and destinations
        are named exactly. Approve resolvers on the{" "}
        <TextLink to="/traffic/gateways">Gateways</TextLink> page; one that stops reporting is taken
        out within minutes.
      </span>
      {canEdit ? null : (
        <Note tone="neutral">
          It moves every machine&apos;s DNS, so changing it takes the DNS permission as well as the
          traffic one.
        </Note>
      )}
      {refusal === "" ? null : <Note tone="danger">{refusal}</Note>}
      {on && refusal === "" && reporters.dnsBlocked === "" && reporters.resolvers.length === 0 ? (
        <Note>
          No approved gateway resolver is answering yet, so the machines still use their usual DNS.
        </Note>
      ) : null}
      {on && reporters.dnsBlocked === "" && reporters.resolvers.length > 0 ? (
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span>Resolvers in use</span>
          <ValueList items={reporters.resolvers} mono />
        </span>
      ) : null}
    </span>
  );
}

function RetentionSection({
  settings,
  canEdit,
}: {
  readonly settings: TrafficSettings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useTrafficSettingsMutation();
  const stored = draftOf(settings);
  const [draft, setDraft] = useState(stored);
  const dirty = retentionKeys.some((key) => draft[key] !== stored[key]);
  const error = retentionError(draft);

  const save = (event: SubmitEvent<HTMLFormElement>): void => {
    event.preventDefault();

    if (error !== null) {
      return;
    }

    update.mutate(
      {
        body: {
          retention: {
            minuteHours: Number(draft.minuteHours),
            hourDays: Number(draft.hourDays),
            dayDays: Number(draft.dayDays),
          },
        },
      },
      {
        onSuccess: (saved) => {
          setDraft(draftOf(saved));
          toast.success("Retention saved");
        },
      },
    );
  };

  return (
    <Section
      title="Retention"
      description="How long each level of detail is kept. Lowering one removes what is older within the hour."
      bodyClassName="p-0"
    >
      <form onSubmit={save}>
        {retentionKeys.map((key) => (
          <SettingRow
            key={key}
            title={labels[key]}
            description={descriptions[key]}
            control={
              <span className="flex items-center gap-2">
                <Input
                  aria-label={labels[key]}
                  className="w-20 text-right tabular-nums"
                  inputMode="numeric"
                  value={draft[key]}
                  disabled={!canEdit}
                  onChange={(event) => {
                    setDraft({ ...draft, [key]: event.target.value.trim() });
                  }}
                />
                <span className="w-10 text-kumo-subtle">{retentionBounds[key].unit}</span>
              </span>
            }
          />
        ))}
        <div className="flex flex-wrap items-center justify-end gap-3 border-t border-kumo-hairline px-5 py-3">
          {dirty && error !== null ? <Note>{error}</Note> : null}
          <Button
            variant="secondary"
            disabled={!dirty}
            onClick={() => {
              setDraft(stored);
            }}
          >
            Reset
          </Button>
          <Button
            type="submit"
            variant="primary"
            disabled={!canEdit || !dirty || error !== null}
            loading={update.isPending}
          >
            Save
          </Button>
        </div>
      </form>
    </Section>
  );
}
