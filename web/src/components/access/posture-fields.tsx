import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Input } from "@cloudflare/kumo/components/input";
import type { Icon } from "@phosphor-icons/react";
import {
  ArrowCircleUpIcon,
  ArrowsClockwiseIcon,
  BarcodeIcon,
  BuildingsIcon,
  DesktopIcon,
  GitBranchIcon,
  GlobeHemisphereWestIcon,
  TagIcon,
} from "@phosphor-icons/react";
import type { ReactElement } from "react";

import {
  expressionExamples,
  isClock,
  weekdayLabels,
  weekdays,
} from "~/components/access/posture-model.ts";
import type { Weekday } from "~/components/access/posture-model.ts";
import { fieldError } from "~/components/ui/field-error.ts";

const tileIconSize = 20;

/** The weekly window of a posture as its editor holds it. */
export interface ScheduleDraft {
  readonly days: readonly Weekday[];
  readonly start: string;
  readonly end: string;
  readonly timezone: string;
}

/** Each ready-made expression's icon, by the label it is listed under. */
const exampleIcons: ReadonlyMap<string, Icon> = new Map([
  ["Current client", ArrowCircleUpIcon],
  ["Stable track", GitBranchIcon],
  ["macOS or Windows", DesktopIcon],
  ["Auto-update on", ArrowsClockwiseIcon],
  ["Known serial", BarcodeIcon],
  ["Custom marker", TagIcon],
  ["Office network", BuildingsIcon],
  ["Country", GlobeHemisphereWestIcon],
]);

/**
 * Ready-made expressions as the dashboard's posture type picker: a two-column grid of tinted 58px
 * tiles, each naming the attribute and showing the whole expression it adds, so two tiles that read
 * the same attribute still tell apart. A country check needs a GeoIP database, and its tile says
 * so.
 */
export function ExampleTiles({
  title,
  description,
  geoIpAvailable,
  onPick,
}: {
  readonly title: string;
  readonly description?: string;
  readonly geoIpAvailable: boolean;
  readonly onPick: (expression: string) => void;
}): ReactElement {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h3 className="font-medium text-kumo-default">{title}</h3>
        {description === undefined ? null : (
          <p className="text-sm text-pretty text-kumo-subtle">{description}</p>
        )}
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        {expressionExamples.map((example) => {
          const needsGeo = example.expression.startsWith("ip:country");
          const disabled = needsGeo && !geoIpAvailable;
          const TileIcon = exampleIcons.get(example.label) ?? DesktopIcon;

          return (
            <button
              key={example.label}
              type="button"
              disabled={disabled}
              {...(disabled
                ? { title: "Needs policy.geoip_database in the server configuration." }
                : {})}
              className="flex min-h-[58px] min-w-0 items-center gap-3 rounded-lg bg-kumo-tint px-4 py-2 text-left hover:bg-kumo-fill focus-visible:ring-2 focus-visible:ring-kumo-focus focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50"
              onClick={() => {
                onPick(example.expression);
              }}
            >
              <TileIcon size={tileIconSize} aria-hidden className="shrink-0 text-kumo-default" />
              <span className="flex min-w-0 flex-col">
                <span className="text-lg font-semibold text-kumo-default">{example.label}</span>
                <span className="font-mono text-xs break-words text-kumo-subtle">
                  {disabled ? "Needs a GeoIP database" : example.expression}
                </span>
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}

export function ScheduleFields({
  draft,
  daysError,
  onChange,
}: {
  readonly draft: ScheduleDraft;
  readonly daysError: string | undefined;
  readonly onChange: (patch: Partial<ScheduleDraft>) => void;
}): ReactElement {
  const toggle = (day: Weekday, checked: boolean): void => {
    onChange({
      days: weekdays.filter((known) => (known === day ? checked : draft.days.includes(known))),
    });
  };

  return (
    <div className="flex flex-col gap-4 rounded-md bg-kumo-tint p-3">
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 font-medium text-kumo-default">Days</legend>
        <div className="flex flex-wrap gap-x-4 gap-y-2">
          {weekdays.map((day) => (
            <Checkbox
              key={day}
              label={weekdayLabels[day]}
              checked={draft.days.includes(day)}
              onCheckedChange={(checked) => {
                toggle(day, checked);
              }}
            />
          ))}
        </div>
        {daysError === undefined ? null : (
          <p className="text-sm leading-snug text-kumo-danger">{daysError}</p>
        )}
      </fieldset>
      <div className="grid items-start gap-4 sm:grid-cols-3">
        <Input
          label="From"
          value={draft.start}
          placeholder="09:00"
          spellCheck={false}
          {...fieldError(isClock(draft.start) ? undefined : "Use HH:MM, such as 09:00.")}
          onChange={(event) => {
            onChange({ start: event.target.value });
          }}
        />
        <Input
          label="To"
          description="An end before the start runs past midnight."
          value={draft.end}
          placeholder="18:00"
          spellCheck={false}
          {...fieldError(isClock(draft.end) ? undefined : "Use HH:MM, such as 18:00.")}
          onChange={(event) => {
            onChange({ end: event.target.value });
          }}
        />
        <Input
          label="Time zone"
          required={false}
          description="An IANA name such as Europe/Berlin. Empty means UTC."
          value={draft.timezone}
          placeholder="Asia/Ho_Chi_Minh"
          spellCheck={false}
          onChange={(event) => {
            onChange({ timezone: event.target.value });
          }}
        />
      </div>
    </div>
  );
}
