import { Button } from "@cloudflare/kumo/components/button";
import { Popover } from "@cloudflare/kumo/components/popover";
import { Select } from "@cloudflare/kumo/components/select";
import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { TabsItem } from "@cloudflare/kumo/components/tabs";
import { CalendarDotsIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { TableToolbar } from "~/components/table/toolbar.tsx";
import { defaultTrafficRange, trafficWindow } from "~/components/traffic/range.ts";
import type { TrafficRange, TrafficWindowSearch } from "~/components/traffic/range.ts";
import { WindowRefusal } from "~/components/traffic/window-refusal.tsx";
import { DateTimeField } from "~/components/ui/date-time-field.tsx";
import { formatAbsolute, parseTime } from "~/lib/time.ts";

const presetTabs: TabsItem[] = [
  { value: "1h", label: "1h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
  { value: "30d", label: "30d" },
  { value: "90d", label: "90d" },
];

/**
 * A search box among the toolbar's controls: it shares a phone's row with the gateway picker rather
 * than taking one of its own.
 */
export const windowSearchClass = "min-w-40 flex-1";

function isPreset(value: string): value is Exclude<TrafficRange, "custom"> {
  return presetTabs.some((tab) => tab.value === value);
}

function pad(part: number): string {
  return String(part).padStart(2, "0");
}

/** An instant as the `YYYY-MM-DDTHH:mm` local time the date field holds. */
export function toLocalField(iso: string): string {
  const date = parseTime(iso);

  if (date === null) {
    return "";
  }

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** The date field's local time back as an instant, or "" when it holds none. */
export function fromLocalField(value: string): string {
  if (value === "") {
    return "";
  }

  const date = new Date(value);

  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

/** The gateway filter's choices: every gateway, then each one that ever reported. */
function gatewayItems(reporters: readonly TrafficReporter[]): { value: string; label: string }[] {
  return [
    { value: "", label: "All gateways" },
    ...reporters.map((reporter) => ({
      value: reporter.nodeId,
      label: reporter.nodeName === "" ? `Gateway ${reporter.nodeId}` : reporter.nodeName,
    })),
  ];
}

/**
 * The controls every traffic page starts with: the window as presets or a custom range, and the
 * gateway to look through. A page adds its own controls as children, after these, and its actions.
 * When the server refuses the window, its reason sits above the controls that change it.
 */
export function WindowToolbar({
  search,
  reporters,
  failure,
  onChange,
  children,
  actions,
}: {
  readonly search: TrafficWindowSearch;
  readonly reporters: readonly TrafficReporter[];
  /** The page's window read's error, if any. */
  readonly failure?: unknown;
  readonly onChange: (next: TrafficWindowSearch) => void;
  readonly children?: ReactNode;
  readonly actions?: ReactNode;
}): ReactElement {
  return (
    <>
      <WindowRefusal
        failure={failure}
        onReset={() => {
          onChange({ ...search, range: defaultTrafficRange, from: "", to: "" });
        }}
      />
      <TableToolbar actions={actions}>
        <Tabs
          variant="segmented"
          aria-label="Time range"
          tabs={presetTabs}
          value={search.range === "custom" ? "" : search.range}
          onValueChange={(value) => {
            if (isPreset(value)) {
              onChange({ ...search, range: value, from: "", to: "" });
            }
          }}
        />
        <CustomRange search={search} onChange={onChange} />
        {reporters.length > 1 || search.gateway !== "" ? (
          <Select
            aria-label="Gateway"
            className="w-40"
            value={search.gateway}
            items={gatewayItems(reporters)}
            onValueChange={(value) => {
              onChange({ ...search, gateway: value ?? "" });
            }}
          />
        ) : null}
        {children}
      </TableToolbar>
    </>
  );
}

/** The custom window: a button that says what it is, and a popover with its two ends. */
function CustomRange({
  search,
  onChange,
}: {
  readonly search: TrafficWindowSearch;
  readonly onChange: (next: TrafficWindowSearch) => void;
}): ReactElement {
  const [open, setOpen] = useState(false);
  const current = trafficWindow(search);
  const [from, setFrom] = useState(toLocalField(current.start));
  const [to, setTo] = useState(toLocalField(current.end));
  const start = fromLocalField(from);
  const end = fromLocalField(to);
  const valid = start !== "" && end !== "" && start < end;
  const custom = search.range === "custom";

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setFrom(toLocalField(current.start));
          setTo(toLocalField(current.end));
        }

        setOpen(next);
      }}
    >
      <Popover.Trigger
        render={<Button variant="secondary" icon={CalendarDotsIcon} aria-label="Custom range" />}
      >
        {custom ? customLabel(current.start, current.end) : "Custom"}
      </Popover.Trigger>
      <Popover.Content className="flex w-80 flex-col gap-4 p-4" align="start">
        <DateTimeField label="From" value={from} onChange={setFrom} />
        <DateTimeField label="To" value={to} onChange={setTo} />
        <div className="flex justify-end gap-2">
          <Button
            variant="secondary"
            onClick={() => {
              setOpen(false);
            }}
          >
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={!valid}
            onClick={() => {
              setOpen(false);
              onChange({ ...search, range: "custom", from: start, to: end });
            }}
          >
            Apply
          </Button>
        </div>
      </Popover.Content>
    </Popover>
  );
}

function customLabel(start: string, end: string): string {
  const from = parseTime(start);
  const to = parseTime(end);

  return from === null || to === null
    ? "Custom"
    : `${formatAbsolute(from)} – ${formatAbsolute(to)}`;
}
