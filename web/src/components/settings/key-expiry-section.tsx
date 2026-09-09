import { Select } from "@cloudflare/kumo/components/select";
import type { ReactElement } from "react";

import type { Settings } from "~/api/queries.ts";
import { useSettingsMutation } from "~/components/settings/mutations.ts";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { Section } from "~/components/ui/section.tsx";
import { toast } from "~/components/ui/toast.ts";

const week = 7;
const month = 30;
const quarter = 90;
const halfYear = 180;
const year = 365;

/** The choices Tailscale offers, plus a year; 0 is off. */
const presets: readonly number[] = [0, 1, week, month, quarter, halfYear, year];

export function keyExpiryLabel(days: number, fallback: number): string {
  if (days > 0) {
    return days === 1 ? "1 day" : `${days} days`;
  }

  return fallback > 0 ? `Client's choice (${fallback} days by default)` : "Client's choice";
}

function optionLabel(days: number): string {
  if (days === 0) {
    return "Off";
  }

  return days === 1 ? "1 day" : `${days} days`;
}

export function KeyExpirySection({
  settings,
  canEdit,
}: {
  readonly settings: Settings;
  readonly canEdit: boolean;
}): ReactElement {
  const update = useSettingsMutation();
  const current = settings.keyExpiryDays;
  const options = presets.includes(current)
    ? presets
    : [...presets, current].toSorted((left, right) => left - right);
  const fallback =
    settings.defaultKeyExpiryDays > 0
      ? `Off. The config file gives new logins ${settings.defaultKeyExpiryDays} days unless the client asks for less.`
      : "Off. A login lasts as long as the client asks for, 180 days by default.";

  return (
    <Section title="Key expiry" description="How long a login stays valid." bodyClassName="p-0">
      <SettingRow
        title="Maximum"
        description={
          current > 0
            ? `A login lasts at most ${optionLabel(current)}, shortened if the client asks for longer. Applies from each machine's next login, never to tagged machines.`
            : fallback
        }
        control={
          <Select
            aria-label="Key expiry"
            value={String(current)}
            disabled={!canEdit || update.isPending}
            renderValue={(value) => optionLabel(Number(value))}
            onValueChange={(value) => {
              const days = Number(value ?? 0);

              update.mutate(
                { body: { keyExpiryDays: days } },
                {
                  onSuccess: () => {
                    toast.success(
                      days === 0 ? "Key expiry off" : `Key expiry set to ${optionLabel(days)}`,
                    );
                  },
                },
              );
            }}
          >
            {options.map((days) => (
              <Select.Option key={days} value={String(days)}>
                {optionLabel(days)}
              </Select.Option>
            ))}
          </Select>
        }
      />
    </Section>
  );
}
