import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { Switch } from "@cloudflare/kumo/components/switch";
import { ArrowsClockwiseIcon, PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import type { Derp } from "~/api/queries.ts";
import {
  frequencyLabel,
  frequencyPresets,
  shortDuration,
  tailscaleMapUrl,
  urlError,
  withAutoUpdate,
  withFrequency,
  withUrl,
  withoutUrl,
} from "~/components/derp/model.ts";
import type { DerpMutations } from "~/components/derp/mutations.ts";
import { EntryList } from "~/components/dns/entry-list.tsx";
import { ValueDialog } from "~/components/dns/value-dialog.tsx";
import { SettingRow } from "~/components/settings/setting-row.tsx";
import { RelativeTime } from "~/components/ui/relative-time.tsx";
import { Section, SectionRow } from "~/components/ui/section.tsx";
import { UrlText } from "~/components/ui/url-text.tsx";

/** The maps the server fetches and merges, and how often. */
export function SourcesSection({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement {
  const [adding, setAdding] = useState(false);
  const settings = derp.effective;
  const pending = mutations.set.isPending;

  return (
    <Section
      title="Map sources"
      description="Maps of relays, fetched and merged in order. A later map's region replaces an earlier one with the same id. Tailscale's public map is the usual first entry."
      bodyClassName="p-0"
      {...(canEdit
        ? {
            actions: (
              <>
                <Button
                  variant="ghost"
                  size="sm"
                  icon={ArrowsClockwiseIcon}
                  loading={mutations.refresh.isPending}
                  onClick={() => {
                    mutations.refresh.mutate({});
                  }}
                >
                  Refetch now
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  icon={PlusIcon}
                  onClick={() => {
                    setAdding(true);
                  }}
                >
                  Add map URL
                </Button>
              </>
            ),
          }
        : {})}
    >
      <EntryList
        canEdit={canEdit}
        pending={pending}
        empty={{
          title: "No map URLs",
          description: "Only the relays below and the config file's map files are served.",
        }}
        entries={settings.urls.map((url) => ({
          key: url,
          value: <UrlText url={url} />,
          aside: url === tailscaleMapUrl ? <Badge variant="secondary">Tailscale</Badge> : undefined,
          removeLabel: `Remove map URL ${url}`,
          onRemove: () => {
            mutations.apply(withoutUrl(settings, url), `Removed ${url}`);
          },
        }))}
      />
      {derp.paths.length === 0 ? null : (
        <SectionRow className="flex flex-col gap-1">
          <span className="font-medium text-kumo-strong">Map files</span>
          <p className="max-w-prose text-kumo-subtle">
            Merged after the URLs, from derp.paths in the config file.
          </p>
          <ul className="flex flex-col gap-0.5 font-mono text-sm">
            {derp.paths.map((path) => (
              <li key={path}>{path}</li>
            ))}
          </ul>
        </SectionRow>
      )}
      <RefetchRow derp={derp} canEdit={canEdit} mutations={mutations} />
      <ValueDialog
        open={adding}
        onOpenChange={setAdding}
        title="Add map URL"
        description="A DERP map in Tailscale's JSON format, such as the one your own control plane or a derper fleet serves."
        label="Map URL"
        placeholder={tailscaleMapUrl}
        validate={urlError}
        successMessage="Map URL added"
        mutation={mutations.set}
        onSubmit={(value, done) => {
          mutations.set.mutate({ body: withUrl(settings, value) }, { onSuccess: done });
        }}
      />
    </Section>
  );
}

/** Whether the map URLs are fetched again on a schedule, and how often. */
function RefetchRow({
  derp,
  canEdit,
  mutations,
}: {
  readonly derp: Derp;
  readonly canEdit: boolean;
  readonly mutations: DerpMutations;
}): ReactElement {
  const settings = derp.effective;
  const pending = mutations.set.isPending;
  const frequency = shortDuration(settings.updateFrequency);
  const options = frequencyPresets.includes(frequency)
    ? frequencyPresets
    : [...frequencyPresets, frequency];

  return (
    <>
      <SettingRow
        title="Refetch on a schedule"
        description={
          <>
            Fetches the map URLs again so new or retired relays reach the machines. When off, the
            map stays as it is until you refetch it here.
            {derp.fetchedAt === "0001-01-01T00:00:00Z" ? null : (
              <>
                {" "}
                Last fetched <RelativeTime value={derp.fetchedAt} />.
              </>
            )}
          </>
        }
        control={
          <Switch
            aria-label="Refetch on a schedule"
            checked={settings.autoUpdate}
            disabled={!canEdit || pending}
            transitioning={pending}
            onCheckedChange={(on) => {
              mutations.apply(
                withAutoUpdate(settings, on),
                `Scheduled refetch ${on ? "on" : "off"}`,
              );
            }}
          />
        }
      />
      <SettingRow
        title="Refetch interval"
        description="How long between two fetches while the schedule is on."
        control={
          <Select
            aria-label="Refetch interval"
            value={frequency}
            disabled={!canEdit || pending || !settings.autoUpdate}
            renderValue={(value) => frequencyLabel(value)}
            onValueChange={(value) => {
              if (value !== null && value !== frequency) {
                mutations.apply(
                  withFrequency(settings, value),
                  `Maps refetched ${frequencyLabel(value).toLowerCase()}`,
                );
              }
            }}
          >
            {options.map((preset) => (
              <Select.Option key={preset} value={preset}>
                {frequencyLabel(preset)}
              </Select.Option>
            ))}
          </Select>
        }
      />
    </>
  );
}
