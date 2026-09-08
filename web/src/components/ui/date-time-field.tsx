import { Button } from "@cloudflare/kumo/components/button";
import { DatePicker } from "@cloudflare/kumo/components/date-picker";
import { Field } from "@cloudflare/kumo/components/field";
import { Popover } from "@cloudflare/kumo/components/popover";
import { Select } from "@cloudflare/kumo/components/select";
import { CalendarDotsIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement, ReactNode } from "react";

import { formatDate, parseTime } from "~/lib/time.ts";

const hoursPerDay = 24;
const minutesPerHour = 60;
/** Minute granularity of the picker: an expiry to the minute is precision nobody asked for. */
const minuteStep = 5;

function pad(part: number): string {
  return String(part).padStart(2, "0");
}

const hours: readonly string[] = Array.from({ length: hoursPerDay }, (_, index) => pad(index));
const minutes: readonly string[] = Array.from({ length: minutesPerHour / minuteStep }, (_, index) =>
  pad(index * minuteStep),
);

interface Parts {
  readonly date: string;
  readonly hour: string;
  readonly minute: string;
}

/** The three parts of a `YYYY-MM-DDTHH:mm` value; each is empty when the value is. */
function split(value: string): Parts {
  const [date = "", clock = ""] = value.split("T");
  const [hour = "", minute = ""] = clock.split(":");

  return { date, hour, minute };
}

function join({ date, hour, minute }: Parts): string {
  return date === "" ? "" : `${date}T${hour}:${minute}`;
}

function calendarValue(date: Date): string {
  return [date.getFullYear(), pad(date.getMonth() + 1), pad(date.getDate())].join("-");
}

/** The clock a freshly picked date starts at: this time of day, on the picker's grid. */
function nowClock(): Pick<Parts, "hour" | "minute"> {
  const now = new Date();

  return {
    hour: pad(now.getHours()),
    minute: pad(Math.floor(now.getMinutes() / minuteStep) * minuteStep),
  };
}

/** Options that always contain the current value, so a value off the grid is never rewritten. */
function withCurrent(options: readonly string[], current: string): readonly string[] {
  return current === "" || options.includes(current) ? options : [...options, current].toSorted();
}

/**
 * A date and a time of day, in the `YYYY-MM-DDTHH:mm` shape a `datetime-local` input holds. The
 * calendar is Kumo's DatePicker in a popover and the clock two selects, because the native control
 * brings the browser's own type scale, borders and locale order into a Kumo form.
 *
 * An empty value means "no time", which every caller reads as never; clearing it is one button.
 */
export function DateTimeField({
  label,
  description,
  required = true,
  value,
  emptyLabel = "Pick a date",
  onChange,
}: {
  readonly label: string;
  readonly description?: ReactNode;
  /** `false` marks the field optional, the same as Kumo's Input. */
  readonly required?: boolean;

  readonly value: string;
  /** What the trigger says while nothing is picked. */
  readonly emptyLabel?: string;
  readonly onChange: (value: string) => void;
}): ReactElement {
  const [open, setOpen] = useState(false);
  const parts = split(value);
  const picked = parseTime(value);

  function pickDate(next: Date | undefined): void {
    setOpen(false);

    if (next === undefined) {
      onChange("");

      return;
    }

    const clock = parts.hour === "" ? nowClock() : parts;

    onChange(join({ date: calendarValue(next), hour: clock.hour, minute: clock.minute }));
  }

  return (
    <Field label={label} required={required} description={description}>
      <div className="flex flex-wrap items-center gap-2">
        <Popover open={open} onOpenChange={setOpen}>
          <Popover.Trigger render={<Button variant="secondary" icon={CalendarDotsIcon} />}>
            {picked === null ? emptyLabel : formatDate(picked)}
          </Popover.Trigger>
          <Popover.Content className="p-3" align="start">
            <DatePicker mode="single" selected={picked ?? undefined} onChange={pickDate} />
          </Popover.Content>
        </Popover>
        <span className="flex items-center gap-1">
          <ClockSelect
            label={`${label} hour`}
            options={withCurrent(hours, parts.hour)}
            value={parts.hour}
            disabled={parts.date === ""}
            onValueChange={(hour) => {
              onChange(join({ ...parts, hour }));
            }}
          />
          <span className="text-kumo-subtle">:</span>
          <ClockSelect
            label={`${label} minute`}
            options={withCurrent(minutes, parts.minute)}
            value={parts.minute}
            disabled={parts.date === ""}
            onValueChange={(minute) => {
              onChange(join({ ...parts, minute }));
            }}
          />
        </span>
        {value === "" ? null : (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              onChange("");
            }}
          >
            Clear
          </Button>
        )}
      </div>
    </Field>
  );
}

function ClockSelect({
  label,
  options,
  value,
  disabled,
  onValueChange,
}: {
  readonly label: string;
  readonly options: readonly string[];
  readonly value: string;
  readonly disabled: boolean;
  readonly onValueChange: (value: string) => void;
}): ReactElement {
  return (
    <Select
      aria-label={label}
      className="w-18"
      value={value === "" ? null : value}
      disabled={disabled}
      placeholder="--"
      renderValue={(current: string | null) => current ?? "--"}
      onValueChange={(next: string | null) => {
        onValueChange(next ?? "00");
      }}
    >
      {options.map((option) => (
        <Select.Option key={option} value={option}>
          {option}
        </Select.Option>
      ))}
    </Select>
  );
}
