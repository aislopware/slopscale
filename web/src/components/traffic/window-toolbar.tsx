import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Popover } from "@cloudflare/kumo/components/popover";
import { Select } from "@cloudflare/kumo/components/select";
import { cn } from "@cloudflare/kumo/utils";
import { CaretUpDownIcon, CheckIcon, ClockIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import type { TrafficReporter } from "~/api/traffic.ts";
import { LogFilters } from "~/components/audit/log-filters.tsx";
import type { LogFilter } from "~/components/audit/log-filters.tsx";
import type { FilterChip } from "~/components/machines/filter-chips.tsx";
import {
  defaultTrafficNetwork,
  defaultTrafficRange,
  trafficNetworks,
  trafficWindow,
} from "~/components/traffic/range.ts";
import type {
  TrafficNetwork,
  TrafficRange,
  TrafficWindowSearch,
} from "~/components/traffic/range.ts";
import { WindowRefusal } from "~/components/traffic/window-refusal.tsx";
import { DateTimeField } from "~/components/ui/date-time-field.tsx";
import { machineLabel } from "~/lib/node.ts";
import { parseTime } from "~/lib/time.ts";

type Preset = Exclude<TrafficRange, "custom">;

const presets: readonly { value: Preset; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
];

const networkLabels: Record<TrafficNetwork, string> = {
  internet: "Internet",
  lan: "LAN",
  all: "All",
};

function isNetwork(value: unknown): value is TrafficNetwork {
  return (trafficNetworks as readonly unknown[]).includes(value);
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
      label:
        reporter.nodeName === ""
          ? `Gateway ${reporter.nodeId}`
          : machineLabel(reporter.nodeName, reporter.nodeOwner),
    })),
  ];
}

/**
 * A filter that came from a click on a row, as a row of the Filters popover: its value is
 * read-only, and its × is the way back out of it.
 */
export function pickedFilter(chip: FilterChip): LogFilter {
  return {
    id: `picked:${chip.name}`,
    label: chip.name,
    operator: "is",
    value: <Input aria-label={chip.name} readOnly value={chip.value} />,
    active: true,
    onRemove: chip.onRemove,
  };
}

const noFilters: readonly LogFilter[] = [];

/** The scope every traffic read has: where the traffic went, and the gateway it went through. */
function windowFilters({
  search,
  reporters,
  networks,
  onChange,
}: {
  readonly search: TrafficWindowSearch;
  readonly reporters: readonly TrafficReporter[];
  readonly networks: boolean;
  readonly onChange: (next: TrafficWindowSearch) => void;
}): LogFilter[] {
  const filters: LogFilter[] = [];

  if (networks) {
    filters.push({
      id: "network",
      label: "Network",
      operator: "is",
      value: (
        <Select
          aria-label="Network"
          className="w-full min-w-0"
          value={search.network}
          items={trafficNetworks.map((network) => ({
            value: network,
            label: networkLabels[network],
          }))}
          onValueChange={(value) => {
            if (isNetwork(value)) {
              onChange({ ...search, network: value });
            }
          }}
        />
      ),
      active: search.network !== defaultTrafficNetwork,
      permanent: true,
      onRemove: () => {
        onChange({ ...search, network: defaultTrafficNetwork });
      },
    });
  }

  if (reporters.length > 1 || search.gateway !== "") {
    filters.push({
      id: "gateway",
      label: "Gateway",
      operator: "is",
      value: (
        <Select
          aria-label="Gateway"
          className="w-full min-w-0"
          value={search.gateway}
          items={gatewayItems(reporters)}
          onValueChange={(value) => {
            onChange({ ...search, gateway: value ?? "" });
          }}
        />
      ),
      active: search.gateway !== "",
      addable: true,
      onRemove: () => {
        onChange({ ...search, gateway: "" });
      },
    });
  }

  return filters;
}

/**
 * The Zero Trust log toolbar for a traffic page, one row: the page's search on the left taking the
 * room left over, then Filters (where the traffic went, the gateway, and whatever the page narrows
 * by) with its count and ×, the time range, and the page's own controls, such as its grouping. On a
 * phone the search takes the first line and the controls the next. When the server refuses the
 * window, its reason sits above the controls that change it.
 */
export function WindowToolbar({
  search,
  reporters,
  networks = true,
  failure,
  onChange,
  query,
  filters = noFilters,
  onClearFilters,
  actions,
}: {
  readonly search: TrafficWindowSearch;
  readonly reporters: readonly TrafficReporter[];
  /** Whether the page's reads split by network; the DNS log does not, so it leaves the choice out. */
  readonly networks?: boolean;
  /** The page's window read's error, if any. */
  readonly failure?: unknown;
  readonly onChange: (next: TrafficWindowSearch) => void;
  /** The page's search box. */
  readonly query?: ReactNode;
  /** The page's own filters, after the network and the gateway. */
  readonly filters?: readonly LogFilter[];
  /**
   * Clears the page's own filters along with the window's scope, handed the window with its scope
   * cleared so the page navigates once. Without it, only the window's scope clears.
   */
  readonly onClearFilters?: (window: TrafficWindowSearch) => void;
  /** The page's controls after the time range. */
  readonly actions?: ReactNode;
}): ReactElement {
  const cleared: TrafficWindowSearch = { ...search, gateway: "", network: defaultTrafficNetwork };

  return (
    <>
      <WindowRefusal
        failure={failure}
        onReset={() => {
          onChange({ ...search, range: defaultTrafficRange, from: "", to: "" });
        }}
      />
      {/* The page's blocks are 28px apart; the toolbar belongs with what it narrows, 12px under it. */}
      <div className="-mb-4 flex flex-wrap items-center gap-2">
        {query === undefined ? null : <div className="flex min-w-0 flex-1 basis-64">{query}</div>}
        <div className={cn("flex flex-wrap items-center gap-2", query === undefined && "ml-auto")}>
          <LogFilters
            filters={[...windowFilters({ search, reporters, networks, onChange }), ...filters]}
            onClear={() => {
              if (onClearFilters === undefined) {
                onChange(cleared);
              } else {
                onClearFilters(cleared);
              }
            }}
          />
          <RangePicker search={search} onChange={onChange} />
          {actions}
        </div>
      </div>
    </>
  );
}

/**
 * A grouping as the toolbar's select, "Group by: Hosts"; on a phone the value alone, so the row
 * keeps the filters, the range and the grouping on one line.
 */
export function GroupBySelect<Value extends string>({
  label,
  tabs,
  value,
  onChange,
}: {
  /** What the rows are, for the select's name: "Group destinations by". */
  readonly label: string;
  readonly tabs: readonly { value: Value; label: string }[];
  readonly value: Value;
  readonly onChange: (value: string) => void;
}): ReactElement {
  return (
    <Select
      aria-label={label}
      className="w-auto min-w-28"
      value={value}
      items={[...tabs]}
      renderValue={(current) => (
        <span className="whitespace-nowrap">
          <span className="max-sm:hidden">Group by: </span>
          {tabs.find((tab) => tab.value === current)?.label ?? ""}
        </span>
      )}
      onValueChange={(next) => {
        onChange(typeof next === "string" ? next : "");
      }}
    />
  );
}

const shortDate = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});

/** "Sep 20, 16:52 → Oct 20, 16:52", the custom range as the log toolbar's button names it. */
function customLabel(start: string, end: string): string {
  const from = parseTime(start);
  const to = parseTime(end);

  return from === null || to === null
    ? "Custom range"
    : `${shortDate.format(from)} → ${shortDate.format(to)}`;
}

/**
 * The time range as the log toolbar's clock button, "Last 24 hours", opening the presets and a
 * custom range with its two ends; a custom range names its ends on the button.
 */
function RangePicker({
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
  const label = custom
    ? customLabel(current.start, current.end)
    : (presets.find((preset) => preset.value === search.range)?.label ?? "");

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
        render={
          <Button
            variant="secondary"
            icon={ClockIcon}
            aria-label={`Time range: ${label}`}
            className="min-w-0"
          />
        }
      >
        <span className="truncate">{label}</span>
        <CaretUpDownIcon aria-hidden className="size-3.5 shrink-0 text-kumo-subtle" />
      </Popover.Trigger>
      <Popover.Content
        className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-3 p-2"
        align="end"
      >
        <Popover.Title className="sr-only">Time range</Popover.Title>
        <div className="flex flex-col">
          {presets.map((preset) => (
            <Button
              key={preset.value}
              variant="ghost"
              aria-pressed={search.range === preset.value}
              className="w-full justify-between"
              onClick={() => {
                setOpen(false);
                onChange({ ...search, range: preset.value, from: "", to: "" });
              }}
            >
              {preset.label}
              {search.range === preset.value ? (
                <CheckIcon aria-hidden className="size-4 text-kumo-link" />
              ) : null}
            </Button>
          ))}
        </div>
        <div className="flex flex-col gap-3 border-t border-kumo-line px-2 pt-3 pb-1">
          <span className="text-sm font-medium">Custom range</span>
          <DateTimeField label="From" value={from} onChange={setFrom} />
          <DateTimeField label="To" value={to} onChange={setTo} />
          <Button
            variant="primary"
            className="self-end"
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
