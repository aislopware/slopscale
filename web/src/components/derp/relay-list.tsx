import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Switch } from "@cloudflare/kumo/components/switch";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import type { ReactElement } from "react";

import { relayDraft } from "~/components/derp/model.ts";
import type { FieldErrors, RelayDraft } from "~/components/derp/model.ts";
import { fieldError } from "~/components/ui/field-error.ts";
import { FormSection } from "~/components/ui/form-page.tsx";

/**
 * Which errors show: those of the fields a save checked; elsewhere, a format error once its field
 * was left with something in it. Fields are named "id", "code" or "<relay key>.<field>".
 */
export interface Shown {
  readonly error: (
    field: string,
    value: string,
    error: string | null | undefined,
  ) => string | undefined;
  readonly leave: (field: string) => void;
}

/** `checked` says whether a save looked at the field, so a relay added after it starts clean. */
export function useShown(checked: (field: string) => boolean): Shown {
  const [left, setLeft] = useState<ReadonlySet<string>>(new Set());

  return {
    error: (field, value, error) =>
      error !== null &&
      error !== undefined &&
      (checked(field) || (left.has(field) && value.trim() !== ""))
        ? error
        : undefined,
    leave: (field) => {
      setLeft((current) => new Set([...current, field]));
    },
  };
}

/**
 * The relays of a region as the dashboard's repeaters list rows: each relay a bordered card of its
 * own with Remove at its top right, and "Add relay" under the last one.
 */
export function RelayList({
  relays,
  errors,
  listError,
  shown,
  onChange,
}: {
  readonly relays: readonly RelayDraft[];
  readonly errors: readonly FieldErrors<RelayDraft>[];
  /** What is wrong with the list as a whole, once a save was tried. */
  readonly listError: string | null;
  readonly shown: Shown;
  readonly onChange: (relays: readonly RelayDraft[]) => void;
}): ReactElement {
  return (
    <FormSection
      title="Relays"
      info="The servers of the region. A machine tries them in order and stays on the first that answers."
    >
      {relays.map((relay, index) => (
        <RelayCard
          key={relay.key}
          position={index + 1}
          relay={relay}
          errors={errors[index] ?? {}}
          shown={shown}
          removable={relays.length > 1}
          onChange={(patch) => {
            onChange(
              relays.map((other) => (other.key === relay.key ? { ...other, ...patch } : other)),
            );
          }}
          onRemove={() => {
            onChange(relays.filter((other) => other.key !== relay.key));
          }}
        />
      ))}
      {listError === null ? null : <p className="text-sm text-kumo-danger">{listError}</p>}
      <div>
        <Button
          variant="ghost"
          size="sm"
          icon={PlusIcon}
          onClick={() => {
            onChange([...relays, relayDraft()]);
          }}
        >
          Add relay
        </Button>
      </div>
    </FormSection>
  );
}

type TextKey = "hostName" | "ipv4" | "ipv6" | "derpPort" | "stunPort" | "name";

function RelayCard({
  position,
  relay,
  errors,
  shown,
  removable,
  onChange,
  onRemove,
}: {
  readonly position: number;
  readonly relay: RelayDraft;
  readonly errors: FieldErrors<RelayDraft>;
  readonly shown: Shown;
  readonly removable: boolean;
  readonly onChange: (patch: Partial<RelayDraft>) => void;
  readonly onRemove: () => void;
}): ReactElement {
  const text = (props: {
    readonly field: TextKey;
    readonly label: string;
    readonly placeholder: string;
    readonly description?: string;
    readonly optional?: boolean;
  }): ReactElement => (
    <RelayInput
      {...props}
      value={relay[props.field]}
      error={shown.error(`${relay.key}.${props.field}`, relay[props.field], errors[props.field])}
      onChange={(value) => {
        onChange({ [props.field]: value });
      }}
      onBlur={() => {
        shown.leave(`${relay.key}.${props.field}`);
      }}
    />
  );

  return (
    <fieldset
      aria-label={`Relay ${position}`}
      className="flex min-w-0 flex-col gap-4 rounded-lg p-4 ring ring-kumo-line"
    >
      <div className="flex min-h-8 items-center justify-between gap-3">
        <span className="font-medium text-kumo-default">Relay {position}</span>
        {removable ? (
          <Button
            variant="ghost"
            size="sm"
            icon={TrashIcon}
            aria-label={`Remove relay ${position}`}
            onClick={onRemove}
          >
            Remove
          </Button>
        ) : null}
      </div>
      <div className="grid items-start gap-4 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        {text({
          field: "hostName",
          label: "Host name",
          placeholder: "derp.example.com",
          description: "The name on its TLS certificate.",
        })}
        {text({
          field: "name",
          label: "Name",
          placeholder: relay.hostName === "" ? "derp1" : relay.hostName,
          description: "Leave empty to use the host name.",
          optional: true,
        })}
      </div>
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {text({
          field: "ipv4",
          label: "IPv4",
          placeholder: "203.0.113.5",
          description: "Published so machines reach the relay when DNS is down.",
          optional: true,
        })}
        {text({
          field: "ipv6",
          label: "IPv6",
          placeholder: "2001:db8::5",
          description: "Published so machines reach the relay when DNS is down.",
          optional: true,
        })}
      </div>
      <div className="grid items-start gap-4 sm:grid-cols-2">
        {text({ field: "derpPort", label: "HTTPS port", placeholder: "443", optional: true })}
        {text({ field: "stunPort", label: "STUN port", placeholder: "3478", optional: true })}
      </div>
      <div className="flex flex-wrap gap-6">
        <Switch
          label="STUN only"
          size="sm"
          checked={relay.stunOnly}
          onCheckedChange={(on) => {
            onChange({ stunOnly: on });
          }}
        />
        <Switch
          label="Serves HTTP on port 80"
          size="sm"
          checked={relay.canPort80}
          onCheckedChange={(on) => {
            onChange({ canPort80: on });
          }}
        />
      </div>
    </fieldset>
  );
}

function RelayInput({
  label,
  placeholder,
  description,
  optional = false,
  value,
  error,
  onChange,
  onBlur,
}: {
  readonly label: string;
  readonly placeholder: string;
  readonly description?: string;
  readonly optional?: boolean;
  readonly value: string;
  readonly error: string | undefined;
  readonly onChange: (value: string) => void;
  readonly onBlur: () => void;
}): ReactElement {
  return (
    <Input
      className="w-full"
      label={label}
      {...(optional ? { required: false } : {})}
      value={value}
      placeholder={placeholder}
      spellCheck={false}
      autoComplete="off"
      {...(description === undefined ? {} : { description })}
      {...fieldError(error)}
      onChange={(event) => {
        onChange(event.target.value);
      }}
      onBlur={onBlur}
    />
  );
}
